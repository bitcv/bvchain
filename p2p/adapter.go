package p2p

import (
	"bvchain/cfg"
	"bvchain/p2p/cxn"
	"bvchain/util"

	"fmt"
	"math"
	"math/rand"
	"net"
	"sync"
	"time"
	"halftwo/mangos/xerr"
)

const (
	_MAX_RANDOM_REDIAL_INTERVAL = 3 * time.Second

	// repeatedly try to reconnect for a few minutes
	// ie. 5 * 20 = 100s
	_RECONNECT_ATTEMPTS = 20
	_RECONNECT_INTERVAL = 5 * time.Second

	// then move into exponential backoff mode for ~1day
	// ie. 3**10 = 16hrs
	_RECONNECT_BACKOFF_ATTEMPTS = 10
	_RECONNECT_BACKOFF_BASE_SECONDS = 3
)

//-----------------------------------------------------------------------------

// An AddrBook represents an address book from the pex package, which is used
// to store peer addresses.
type AddrBook interface {
	AddAddress(addr *NetAddress, src *NetAddress) error
	AddOurAddress(*NetAddress)
	IsOurAddress(*NetAddress) bool
	MarkGood(*NetAddress)
	RemoveAddress(*NetAddress)
	HasAddress(*NetAddress) bool
	Save()
}

//-----------------------------------------------------------------------------

// Adapter handles peer connections and exposes an API to receive incoming messages
// on `Reactors`.  Each `Reactor` is responsible for handling incoming messages of one
// or more `Channels`.  So while sending outgoing messages is typically performed on the peer,
// incoming messages are received on the reactor.
type Adapter struct {
	util.BaseService

	config       *cfg.P2pConfig
	listeners    []Listener
	reactors     map[string]Reactor
	chDescs      []*cxn.ChannelDescriptor
	reactorsByCh map[byte]Reactor
	peers        *PeerSet
	dialing      *util.StringSet
	reconnecting *util.StringSet
	nodeInfo     *NodeInfo // our node info
	nodeKey      *NodeKey // our node privkey
	addrBook     AddrBook

	filterConnByAddr func(net.Addr) error
	filterConnById   func(NodeId) error

	mxConfig cxn.MxConfig

	rng *rand.Rand
}

// AdapterOption sets an optional parameter on the Adapter.
type AdapterOption func(*Adapter)

// NewAdapter creates a new Adapter with the given config.
func NewAdapter(config *cfg.P2pConfig, options ...AdapterOption) *Adapter {
	ap := &Adapter{
		config:       config,
		reactors:     make(map[string]Reactor),
		chDescs:      make([]*cxn.ChannelDescriptor, 0),
		reactorsByCh: make(map[byte]Reactor),
		peers:        NewPeerSet(),
		dialing:      util.NewStringSet(),
		reconnecting: util.NewStringSet(),
	}
	ap.BaseService.Init(nil, "P2P Adapter", ap)

	// Ensure we have a completely undeterministic PRNG.
	ap.rng = util.NewRand()

	mxConfig := cxn.DefaultMxConfig()
	mxConfig.FlushThrottle = time.Duration(config.FlushThrottleTimeout) * time.Millisecond
	mxConfig.SendRate = config.SendRate
	mxConfig.RecvRate = config.RecvRate
	mxConfig.MaxPacketPayloadSize = config.MaxPacketPayloadSize

	ap.mxConfig = mxConfig

	for _, option := range options {
		option(ap)
	}

	return ap
}

//---------------------------------------------------------------------
// Adapter setup

// AddReactor adds the given reactor to the adapter.
// NOTE: Not goroutine safe.
func (ap *Adapter) AddReactor(name string, reactor Reactor) Reactor {
	// Validate the reactor.
	// No two reactors can share the same channel.
	reactorChannels := reactor.GetChannels()
	for _, chDesc := range reactorChannels {
		chId := chDesc.Id
		if ap.reactorsByCh[chId] != nil {
			panic(fmt.Sprintf("Channel %X has multiple reactors %v & %v", chId, ap.reactorsByCh[chId], reactor))
		}
		ap.chDescs = append(ap.chDescs, chDesc)
		ap.reactorsByCh[chId] = reactor

		if ap.nodeInfo != nil {
			ap.nodeInfo.Channels = append(ap.nodeInfo.Channels, chId)
		}
	}
	ap.reactors[name] = reactor
	reactor.SetAdapter(ap)
	return reactor
}

// Reactors returns a map of reactors registered on the adapter.
// NOTE: Not goroutine safe.
func (ap *Adapter) Reactors() map[string]Reactor {
	return ap.reactors
}

// GetReactor returns the reactor with the given name.
// NOTE: Not goroutine safe.
func (ap *Adapter) GetReactor(name string) Reactor {
	return ap.reactors[name]
}

// AddListener adds the given listener to the adapter for listening to incoming peer connections.
// NOTE: Not goroutine safe.
func (ap *Adapter) AddListener(l Listener) {
	ap.listeners = append(ap.listeners, l)
}

// Listeners returns the list of listeners the adapter listens on.
// NOTE: Not goroutine safe.
func (ap *Adapter) Listeners() []Listener {
	return ap.listeners
}

// IsListening returns true if the adapter has at least one listener.
// NOTE: Not goroutine safe.
func (ap *Adapter) IsListening() bool {
	return len(ap.listeners) > 0
}

// SetNodeInfo sets the adapter's NodeInfo for checking compatibility and handshaking with other nodes.
// NOTE: Not goroutine safe.
func (ap *Adapter) SetNodeInfo(nodeInfo *NodeInfo) {
	ni := *nodeInfo
	ni.Channels = make([]byte, 0, len(ap.chDescs))
	for _, ch := range ap.chDescs {
		ni.Channels = append(ni.Channels, ch.Id)
	}
	ap.nodeInfo = &ni
}

// NodeInfo returns the adapter's NodeInfo.
// NOTE: Not goroutine safe.
func (ap *Adapter) NodeInfo() *NodeInfo {
	return ap.nodeInfo
}

// SetNodeKey sets the adapter's private key for authenticated encryption.
// NOTE: Not goroutine safe.
func (ap *Adapter) SetNodeKey(nodeKey *NodeKey) {
	ap.nodeKey = nodeKey
}

//---------------------------------------------------------------------
// Service start/stop

// OnStart implements BaseService. It starts all the reactors, peers, and listeners.
func (ap *Adapter) OnStart() error {
	// Start reactors
	for _, reactor := range ap.reactors {
		err := reactor.Start()
		if err != nil {
			return xerr.Tracef(err, "failed to start %v", reactor)
		}
	}
	// Start listeners
	for _, listener := range ap.listeners {
		go ap.listenerRoutine(listener)
	}
	return nil
}

// OnStop implements BaseService. It stops all listeners, peers, and reactors.
func (ap *Adapter) OnStop() {
	// Stop listeners
	for _, listener := range ap.listeners {
		listener.Stop()
	}
	ap.listeners = nil
	// Stop peers
	for _, peer := range ap.peers.List() {
		peer.Stop()
		ap.peers.Remove(peer)
	}
	// Stop reactors
	ap.Logger.Debug("Adapter: Stopping reactors")
	for _, reactor := range ap.reactors {
		reactor.Stop()
	}
}

//---------------------------------------------------------------------
// Peers

// Broadcast runs a go routine for each attempted send, which will block trying
// to send for defaultSendTimeoutSeconds. Returns a channel which receives
// success values for each attempted send (false if times out). Channel will be
// closed once msg bytes are sent to all peers (or time out).
//
// NOTE: Broadcast uses goroutines, so order of broadcast may not be preserved.
func (ap *Adapter) Broadcast(chId byte, msgBytes []byte) chan bool {
	successChan := make(chan bool, len(ap.peers.List()))
	ap.Logger.Debug("Broadcast", "channel", chId, "msgBytes", fmt.Sprintf("%X", msgBytes))
	var wg sync.WaitGroup
	for _, peer := range ap.peers.List() {
		wg.Add(1)
		go func(peer Peer) {
			defer wg.Done()
			success := peer.Send(chId, msgBytes)
			successChan <- success
		}(peer)
	}
	go func() {
		wg.Wait()
		close(successChan)
	}()
	return successChan
}

// NumPeers returns the count of outging/incoming and outgoing-dialing peers.
func (ap *Adapter) NumPeers() (outgoing, incoming, dialing int) {
	peers := ap.peers.List()
	for _, peer := range peers {
		if peer.IsOutgoing() {
			outgoing++
		} else {
			incoming++
		}
	}
	dialing = ap.dialing.Size()
	return
}

// Peers returns the set of peers that are connected to the adapter.
func (ap *Adapter) Peers() ImmutablePeerSet {
	return ap.peers
}

// StopPeerForError disconnects from a peer due to external error.
// If the peer is persistent, it will attempt to reconnect.
// TODO: make record depending on reason.
func (ap *Adapter) StopPeerForError(peer Peer, reason interface{}) {
	ap.Logger.Error("Stopping peer for error", "peer", peer, "err", reason)
	ap.stopAndRemovePeer(peer, reason)

	if peer.IsPersistent() {
		// NOTE: this is the self-reported addr, not the original we dialed
		go ap.reconnectToPeer(peer.NodeInfo().NetAddress())
	}
}

// StopPeerGracefully disconnects from a peer gracefully.
// TODO: handle graceful disconnects.
func (ap *Adapter) StopPeerGracefully(peer Peer) {
	ap.Logger.Info("Stopping peer gracefully")
	ap.stopAndRemovePeer(peer, nil)
}

func (ap *Adapter) stopAndRemovePeer(peer Peer, reason interface{}) {
	ap.peers.Remove(peer)
	peer.Stop()
	for _, reactor := range ap.reactors {
		reactor.RemovePeer(peer, reason)
	}
}

// reconnectToPeer tries to reconnect to the addr, first repeatedly
// with a fixed interval, then with exponential backoff.
// If no success after all that, it stops trying, and leaves it
// to the PEX/Addrbook to find the peer with the addr again
// NOTE: this will keep trying even if the handshake or auth fails.
// TODO: be more explicit with error types so we only retry on certain failures
//  - ie. if we're getting ErrDuplicatePeer we can stop
//  	because the addrbook got us the peer back already
func (ap *Adapter) reconnectToPeer(addr *NetAddress) {
	if ap.reconnecting.Has(string(addr.NodeId)) {
		return
	}
	ap.reconnecting.Add(string(addr.NodeId))
	defer ap.reconnecting.Remove(string(addr.NodeId))

	start := time.Now()
	ap.Logger.Info("Reconnecting to peer", "addr", addr)
	for i := 0; i < _RECONNECT_ATTEMPTS; i++ {
		if !ap.IsRunning() {
			return
		}

		err := ap.DialPeerWithAddress(addr, true)
		if err == nil {
			return // success
		}

		ap.Logger.Info("Error reconnecting to peer. Trying again", "tries", i, "err", err, "addr", addr)
		// sleep a set amount
		ap.randomSleep(_RECONNECT_INTERVAL)
		continue
	}

	ap.Logger.Error("Failed to reconnect to peer. Beginning exponential backoff",
		"addr", addr, "elapsed", time.Since(start))
	for i := 0; i < _RECONNECT_BACKOFF_ATTEMPTS; i++ {
		if !ap.IsRunning() {
			return
		}

		// sleep an exponentially increasing amount
		sleepIntervalSeconds := math.Pow(_RECONNECT_BACKOFF_BASE_SECONDS, float64(i))
		ap.randomSleep(time.Duration(sleepIntervalSeconds) * time.Second)
		err := ap.DialPeerWithAddress(addr, true)
		if err == nil {
			return // success
		}
		ap.Logger.Info("Error reconnecting to peer. Trying again", "tries", i, "err", err, "addr", addr)
	}
	ap.Logger.Error("Failed to reconnect to peer. Giving up", "addr", addr, "elapsed", time.Since(start))
}

// SetAddrBook allows to set address book on Adapter.
func (ap *Adapter) SetAddrBook(addrBook AddrBook) {
	ap.addrBook = addrBook
}

// MarkPeerAsGood marks the given peer as good when it did something useful
// like contributed to consensus.
func (ap *Adapter) MarkPeerAsGood(peer Peer) {
	if ap.addrBook != nil {
		ap.addrBook.MarkGood(peer.NodeInfo().NetAddress())
	}
}

//---------------------------------------------------------------------
// Dialing

// IsDialing returns true if the adapter is currently dialing the given NodeId.
func (ap *Adapter) IsDialing(id NodeId) bool {
	return ap.dialing.Has(string(id))
}

// DialPeersAsync dials a list of peers asynchronously in random order (optionally, making them persistent).
// Used to dial peers from config on startup or from unsafe-RPC (trusted sources).
// TODO: remove addrBook arg since it's now set on the adapter
func (ap *Adapter) DialPeersAsync(addrBook AddrBook, peers []string, persistent bool) error {
	netAddrs, errs := NewNetAddressStrings(peers)
	// only log errors, dial correct addresses
	for _, err := range errs {
		ap.Logger.Error("Error in peer's address", "err", err)
	}

	ourAddr := ap.nodeInfo.NetAddress()

	// TODO: this code feels like it's in the wrong place.
	// The integration tests depend on the addrBook being saved
	// right away but maybe we can change that. Recall that
	// the addrBook is only written to disk every 2min
	if addrBook != nil {
		// add peers to `addrBook`
		for _, netAddr := range netAddrs {
			// do not add our address or Id
			if !netAddr.IsSame(ourAddr) {
				if err := addrBook.AddAddress(netAddr, ourAddr); err != nil {
					ap.Logger.Error("Can't add peer's address to addrbook", "err", err)
				}
			}
		}
		// Persist some peers to disk right away.
		// NOTE: integration tests depend on this
		addrBook.Save()
	}

	// permute the list, dial them in random order.
	perm := ap.rng.Perm(len(netAddrs))
	for i := 0; i < len(perm); i++ {
		go func(i int) {
			j := perm[i]

			addr := netAddrs[j]
			// do not dial ourselves
			if addr.IsSame(ourAddr) {
				return
			}

			ap.randomSleep(0)
			err := ap.DialPeerWithAddress(addr, persistent)
			if err != nil {
				switch err.(type) {
				case ErrAdapterConnectToSelf, ErrAdapterDuplicatePeerId:
					ap.Logger.Debug("Error dialing peer", "err", err)
				default:
					ap.Logger.Error("Error dialing peer", "err", err)
				}
			}
		}(i)
	}
	return nil
}

// DialPeerWithAddress dials the given peer and runs ap.addPeerConn if it connects and authenticates successfully.
// If `persistent == true`, the adapter will always try to reconnect to this peer if the connection ever fails.
func (ap *Adapter) DialPeerWithAddress(addr *NetAddress, persistent bool) error {
	ap.dialing.Add(string(addr.NodeId))
	defer ap.dialing.Remove(string(addr.NodeId))
	return ap.addOutgoingPeerWithConfig(addr, ap.config, persistent)
}

// sleep for interval plus some extra amount random on [0, _MAX_RANDOM_REDIAL_INTERVAL)
func (ap *Adapter) randomSleep(interval time.Duration) {
	r := time.Duration(ap.rng.Int63n(int64(_MAX_RANDOM_REDIAL_INTERVAL)))
	time.Sleep(r + interval)
}

//------------------------------------------------------------------------------------
// Connection filtering

// FilterConnByAddr returns an error if connecting to the given address is forbidden.
func (ap *Adapter) FilterConnByAddr(addr net.Addr) error {
	if ap.filterConnByAddr != nil {
		return ap.filterConnByAddr(addr)
	}
	return nil
}

// FilterConnById returns an error if connecting to the given peer NodeId is forbidden.
func (ap *Adapter) FilterConnById(id NodeId) error {
	if ap.filterConnById != nil {
		return ap.filterConnById(id)
	}
	return nil

}

// SetAddrFilter sets the function for filtering connections by address.
func (ap *Adapter) SetAddrFilter(f func(net.Addr) error) {
	ap.filterConnByAddr = f
}

// SetIdFilter sets the function for filtering connections by peer NodeId.
func (ap *Adapter) SetIdFilter(f func(NodeId) error) {
	ap.filterConnById = f
}

//------------------------------------------------------------------------------------

func (ap *Adapter) listenerRoutine(l Listener) {
	for {
		inConn, ok := <-l.C4Conn()
		if !ok {
			break
		}

		maxPeers := ap.config.MaxNumPeers - ap.config.MinNumOutgoing
		if maxPeers <= ap.peers.Size() {
			ap.Logger.Info("Ignoring incoming connection: already have enough peers", "address", inConn.RemoteAddr().String(), "numPeers", ap.peers.Size(), "max", maxPeers)
			continue
		}

		// New incoming connection!
		err := ap.addIncomingPeerWithConfig(inConn, ap.config)
		if err != nil {
			ap.Logger.Info("Ignoring incoming connection: error while adding peer", "address", inConn.RemoteAddr().String(), "err", err)
			continue
		}
	}

	// cleanup
}

func (ap *Adapter) addIncomingPeerWithConfig(
	conn net.Conn,
	config *cfg.P2pConfig,
) error {
	peerConn, err := newConnectionIncoming(config, ap.nodeKey.PrivKey, conn)
	if err != nil {
		conn.Close() // peer is nil
		return err
	}
	if err = ap.addPeerConn(peerConn); err != nil {
		peerConn.CloseConn()
		return err
	}

	return nil
}

// dial the peer; make secret connection; authenticate against the dialed Id;
// add the peer.
// if dialing fails, start the reconnect loop. If handhsake fails, its over.
// If peer is started succesffuly, reconnectLoop will start when
// StopPeerForError is called
func (ap *Adapter) addOutgoingPeerWithConfig(
	addr *NetAddress,
	config *cfg.P2pConfig,
	persistent bool,
) error {
	ap.Logger.Info("Dialing peer", "address", addr)
	peerConn, err := newConnectionOutgoing(config, ap.nodeKey.PrivKey, addr, persistent)
	if err != nil {
		if persistent {
			go ap.reconnectToPeer(addr)
		}
		return err
	}

	if err := ap.addPeerConn(peerConn); err != nil {
		peerConn.CloseConn()
		return err
	}
	return nil
}

// addPeerConn performs the P2P handshake with a peer
// that already has a SecretConnection. If all goes well,
// it starts the peer and adds it to the adapter.
// NOTE: This performs a blocking handshake before the peer is added.
// NOTE: If error is returned, caller is responsible for calling
// peer.CloseConn()
func (ap *Adapter) addPeerConn(pc *_PeerConnection) error {

	addr := pc.conn.RemoteAddr()
	if err := ap.FilterConnByAddr(addr); err != nil {
		return err
	}

	// Exchange NodeInfo on the conn
	peerNodeInfo, err := pc.Handshake(ap.nodeInfo, time.Duration(ap.config.HandshakeTimeout))
	if err != nil {
		return err
	}

	peerId := peerNodeInfo.NodeId

	// ensure connection key matches self reported key
	connId := pc.NodeId()

	if peerId != connId {
		return fmt.Errorf(
			"nodeInfo.NodeId() (%v) doesn't match conn.NodeId() (%v)",
			peerId,
			connId,
		)
	}

	// Validate the peers nodeInfo
	if err := peerNodeInfo.Validate(); err != nil {
		return err
	}

	// Avoid self
	if ap.nodeKey.NodeId() == peerId {
		addr := peerNodeInfo.NetAddress()
		// remove the given address from the address book
		// and add to our addresses to avoid dialing again
		ap.addrBook.RemoveAddress(addr)
		ap.addrBook.AddOurAddress(addr)
		return ErrAdapterConnectToSelf{addr}
	}

	// Avoid duplicate
	if ap.peers.Has(peerId) {
		return ErrAdapterDuplicatePeerId{peerId}
	}

	// Check for duplicate connection or peer info Ip.
	if !ap.config.AllowDuplicateIp &&
		(ap.peers.HasIp(pc.RemoteIp()) ||
			ap.peers.HasIp(peerNodeInfo.NetAddress().Ip)) {
		return ErrAdapterDuplicatePeerIp{pc.RemoteIp()}
	}

	// Filter peer against NodeId white list
	if err := ap.FilterConnById(peerId); err != nil {
		return err
	}

	// Check version, chain id
	if err := ap.nodeInfo.CompatibleWith(peerNodeInfo); err != nil {
		return err
	}

	peer := newPeer(pc, ap.mxConfig, peerNodeInfo, ap.reactorsByCh, ap.chDescs, ap.StopPeerForError)
	peer.SetLogger(ap.Logger.With("peer", addr))

	peer.Logger.Info("Successful handshake with peer", "peerNodeInfo", peerNodeInfo)

	// All good. Start peer
	if ap.IsRunning() {
		if err = ap.startInitPeer(peer); err != nil {
			return err
		}
	}

	// Add the peer to .peers.
	// We start it first so that a peer in the list is safe to Stop.
	// It should not err since we already checked peers.Has().
	if err := ap.peers.Add(peer); err != nil {
		return err
	}

	ap.Logger.Info("Added peer", "peer", peer)
	return nil
}

func (ap *Adapter) startInitPeer(peer *_Peer) error {
	err := peer.Start() // spawn send/recv routines
	if err != nil {
		// Should never happen
		ap.Logger.Error("Error starting peer", "peer", peer, "err", err)
		return err
	}

	for _, reactor := range ap.reactors {
		reactor.AddPeer(peer)
	}

	return nil
}

