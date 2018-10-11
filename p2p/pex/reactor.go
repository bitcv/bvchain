package pex

import (
	"bvchain/util"
	"bvchain/p2p"
	"bvchain/p2p/cxn"

	"errors"
	"fmt"
	"reflect"
	"sort"
	"sync"
	"time"
)

type Peer = p2p.Peer

// over-estimate of max NetAddress size
// NodeId (52) + Ip (16) + Port (2) + ...
const _MAX_ADDRESS_SIZE = 256

// NOTE: amplificaiton factor!
// small request results in up to _MAX_MSG_SIZE response
const _MAX_MSG_SIZE = _MAX_ADDRESS_SIZE * _MAX_GET_SELECTION

// ensure we have enough peers
const _DEFAULT_ENSURE_PEERS_PERIOD   = 30 * time.Second
const _DEFAULT_MIN_NUM_OUTGOING_PEERS = 10

// Seed/Crawler constants

// We want seeds to only advertise good peers. Therefore they should wait at
// least as long as we expect it to take for a peer to become good before
// disconnecting.
// see consensus/reactor.go: blocksToContributeToBecomeGoodPeer
// 10000 blocks assuming 1s blocks ~ 2.7 hours.
const _SEED_DISCONNECT_WAIT_PERIOD = 3 * time.Hour

const _CRAWL_PEER_INTERVAL = 2 * time.Minute // don't redial for this. TODO: back-off. what for?

const _CRAWL_PEER_PERIOD = 30 * time.Second // check some peers every this

const _MAX_ATTEMPTS_TO_DIAL = 16 // ~ 35h in total (last attempt - 18h)

// if node connects to seed, it does not have any trusted peers.
// Especially in the beginning, node should have more trusted peers than
// untrusted.
const _BIAS_TO_NEW_PEERS = 30 // 70 to select good peers

// PexReactor handles PEX (peer exchange) and ensures that an
// adequate number of peers are connected to the adapter.
//
// It uses `AddrBook` (address book) to store `NetAddress`es of the peers.
//
// ## Preventing abuse
//
// Only accept _PexAddrsMessage from peers we sent a corresponding _PexRequestMessage too.
// Only accept one _PexRequestMessage every ~_DEFAULT_ENSURE_PEERS_PERIOD.
type PexReactor struct {
	p2p.BaseReactor

	book AddrBook
	config *PexReactorConfig
	ensurePeersPeriod time.Duration // TODO: should go in the config

	// maps to prevent abuse
	requestsSent *util.StringSet // unanswered send requests
	lastReceivedRequests *util.StringMap // NodeId->time.Time: last time peer requested from us

	attemptsToDial sync.Map // address (string) -> {number of attempts (int), last time dialed (time.Time)}
}

func (r *PexReactor) minReceiveRequestInterval() time.Duration {
	// NOTE: must be less than ensurePeersPeriod, otherwise we'll request
	// peers too quickly from others and they'll think we're bad!
	return r.ensurePeersPeriod / 3
}

// PexReactorConfig holds reactor specific configuration data.
type PexReactorConfig struct {
	// Seed/Crawler mode
	SeedMode bool

	// Seeds is a list of addresses reactor may use
	// if it can't connect to peers in the addrbook.
	Seeds []string

	// PrivatePeerIds is a list of peer Ids, which must not be gossiped to other
	// peers.
	PrivatePeerIds []string
}

type _AttemptsToDial struct {
	number     int
	lastDialed time.Time
}

// NewPexReactor creates new PEX reactor.
func NewPexReactor(b AddrBook, config *PexReactorConfig) *PexReactor {
	r := &PexReactor{
		book: b,
		config: config,
		ensurePeersPeriod: _DEFAULT_ENSURE_PEERS_PERIOD,
		requestsSent: util.NewStringSet(),
		lastReceivedRequests: util.NewStringMap(),
	}
	r.BaseReactor.Init("PexReactor", r)
	return r
}

// OnStart implements BaseService
func (r *PexReactor) OnStart() error {
	if err := r.BaseReactor.OnStart(); err != nil {
		return err
	}
	err := r.book.Start()
	if err != nil && err != util.ErrAlreadyStarted {
		return err
	}

	// return err if user provided a bad seed address
	// or a host name that we cant resolve
	if err := r.checkSeeds(); err != nil {
		return err
	}

	// Check if this node should run
	// in seed/crawler mode
	if r.config.SeedMode {
		go r.crawlPeersRoutine()
	} else {
		go r.ensurePeersRoutine()
	}
	return nil
}

// OnStop implements BaseService
func (r *PexReactor) OnStop() {
	r.BaseReactor.OnStop()
	r.book.Stop()
}

// GetChannels implements Reactor
func (r *PexReactor) GetChannels() []*cxn.ChannelDescriptor {
	return []*cxn.ChannelDescriptor{
		{Id:p2p.PEX_CHANNEL, Priority:1, SendQueueCapacity:10,},
	}
}

// AddPeer implements Reactor by adding peer to the address book (if incoming)
// or by requesting more addresses (if outgoing).
func (r *PexReactor) AddPeer(p Peer) {
	if p.IsOutgoing() {
		// For outgoing peers, the address is already in the books -
		// either via DialPeersAsync or r.ReceiveMessage.
		// Ask it for more peers if we need.
		if r.book.NeedMoreAddrs() {
			r.RequestAddrs(p)
		}
	} else {
		// incoming peer is its own source
		addr := p.NodeInfo().NetAddress()
		src := addr

		if r.isAddrPrivate(addr) {
			return
		}

		// add to book. dont RequestAddrs right away because
		// we don't trust incoming as much - let ensurePeersRoutine handle it.
		err := r.book.AddAddress(addr, src)
		r.logErrAddrBook(err)
	}
}

func (r *PexReactor) logErrAddrBook(err error) {
	if err != nil {
		switch err.(type) {
		case ErrAddrBookNilAddr:
			r.Logger.Error("Failed to add new address", "err", err)
		default:
			// non-routable, self, full book, etc.
			r.Logger.Debug("Failed to add new address", "err", err)
		}
	}
}

// RemovePeer implements Reactor.
func (r *PexReactor) RemovePeer(p Peer, reason interface{}) {
	id := string(p.NodeId())
	r.requestsSent.Remove(id)
	r.lastReceivedRequests.Remove(id)
}

// ReceiveMessage implements Reactor by handling incoming PEX messages.
func (r *PexReactor) ReceiveMessage(src Peer, chId byte, msgBytes []byte) {
	msg, err := p2p.TheMK.Unmarshal(chId, msgBytes)
	if err != nil {
		r.Logger.Error("Error decoding message", "src", src, "chId", chId, "msg", msg, "err", err, "bytes", msgBytes)
		r.Adapter.StopPeerForError(src, err)
		return
	}
	r.Logger.Debug("Received message", "src", src, "chId", chId, "msg", msg)

	switch msg := msg.(type) {
	case *_PexRequestMessage:
		// Check we're not receiving too many requests
		if err := r.receiveRequest(src); err != nil {
			r.Adapter.StopPeerForError(src, err)
			return
		}

		// Seeds disconnect after sending a batch of addrs
		// NOTE: this is a prime candidate for amplification attacks
		// so it's important we
		// 1) restrict how frequently peers can request
		// 2) limit the output size
		if r.config.SeedMode {
			r.SendAddrs(src, r.book.GetSelectionWithBias(_BIAS_TO_NEW_PEERS))
			r.Adapter.StopPeerGracefully(src)
		} else {
			r.SendAddrs(src, r.book.GetSelection())
		}

	case *_PexAddrsMessage:
		if err := r.ReceiveAddrs(src, msg.Addrs); err != nil {
			r.Adapter.StopPeerForError(src, err)
			return
		}
	default:
		r.Logger.Error(fmt.Sprintf("Unknown message type %v", reflect.TypeOf(msg)))
	}
}

// enforces a minimum amount of time between requests
func (r *PexReactor) receiveRequest(src Peer) error {
	id := string(src.NodeId())
	v := r.lastReceivedRequests.Get(id)
	if v == nil {
		r.lastReceivedRequests.Set(id, time.Time{})
		return nil
	}

	lastReceived := v.(time.Time)
	now := time.Now()
	minInterval := r.minReceiveRequestInterval()
	if now.Sub(lastReceived) < minInterval {
		return fmt.Errorf("Peer (%v) sent next PEX request too soon. lastReceived: %v, now: %v, minInterval: %v. Disconnecting",
			src.NodeId(),
			lastReceived,
			now,
			minInterval,
		)
	}
	r.lastReceivedRequests.Set(id, now)
	return nil
}

// RequestAddrs asks peer for more addresses if we do not already
// have a request out for this peer.
func (r *PexReactor) RequestAddrs(p Peer) {
	r.Logger.Debug("Request addrs", "from", p)
	id := string(p.NodeId())
	if r.requestsSent.Has(id) {
		return
	}
	r.requestsSent.Add(id)
	p.Send(p2p.PEX_CHANNEL, p2p.TheMK.MustMarshal(p2p.PEX_CHANNEL, &_PexRequestMessage{}))
}

// ReceiveAddrs adds the given addrs to the addrbook if theres an open
// request for this peer and deletes the open request.
// If there's no open request for the src peer, it returns an error.
func (r *PexReactor) ReceiveAddrs(src Peer, addrs []*p2p.NetAddress) error {

	id := string(src.NodeId())
	if !r.requestsSent.Has(id) {
		return errors.New("Received unsolicited _PexAddrsMessage")
	}
	r.requestsSent.Remove(id)

	srcAddr := src.NodeInfo().NetAddress()
	for _, netAddr := range addrs {
		// NOTE: AddrBook.GetSelection method should never return nil addrs
		if netAddr == nil {
			return errors.New("received nil addr")
		}

		if r.isAddrPrivate(netAddr) {
			continue
		}

		err := r.book.AddAddress(netAddr, srcAddr)
		r.logErrAddrBook(err)
	}
	return nil
}

// SendAddrs sends addrs to the peer.
func (r *PexReactor) SendAddrs(p Peer, netAddrs []*p2p.NetAddress) {
	p.Send(p2p.PEX_CHANNEL, p2p.TheMK.MustMarshal(p2p.PEX_CHANNEL, &_PexAddrsMessage{Addrs:netAddrs}))
}

// SetEnsurePeersPeriod sets period to ensure peers connected.
func (r *PexReactor) SetEnsurePeersPeriod(d time.Duration) {
	r.ensurePeersPeriod = d
}

// Ensures that sufficient peers are connected. (continuous)
func (r *PexReactor) ensurePeersRoutine() {
	rng := util.NewRand()
	jitter := rng.Int63n(r.ensurePeersPeriod.Nanoseconds())

	// Randomize first round of communication to avoid thundering herd.
	// If no potential peers are present directly start connecting so we guarantee
	// swift setup with the help of configured seeds.
	if r.hasPotentialPeers() {
		time.Sleep(time.Duration(jitter))
	}

	// fire once immediately.
	// ensures we dial the seeds right away if the book is empty
	r.ensurePeers()

	// fire periodically
	ticker := time.NewTicker(r.ensurePeersPeriod)
	for {
		select {
		case <-ticker.C:
			r.ensurePeers()

		case <-r.C4Quit():
			ticker.Stop()
			return
		}
	}
}

// ensurePeers ensures that sufficient peers are connected. (once)
//
// heuristic that we haven't perfected yet, or, perhaps is manually edited by
// the node operator. It should not be used to compute what addresses are
// already connected or not.
func (r *PexReactor) ensurePeers() {
	out, in, dial := r.Adapter.NumPeers()
	numToDial := _DEFAULT_MIN_NUM_OUTGOING_PEERS - (out + dial)

	r.Logger.Info(
		"Ensure peers",
		"numOutPeers", out,
		"numInPeers", in,
		"numDialing", dial,
		"numToDial", numToDial,
	)

	if numToDial <= 0 {
		return
	}

	// bias to prefer more vetted peers when we have fewer connections.
	// not perfect, but somewhate ensures that we prioritize connecting to more-vetted
	// NOTE: range here is [10, 90]. Too high ?
	newBias := util.MinInt(out, 8)*10 + 10

	toDial := make(map[p2p.NodeId]*p2p.NetAddress)
	// Try maxAttempts times to pick numToDial addresses to dial
	maxAttempts := numToDial * 3

	for i := 0; i < maxAttempts && len(toDial) < numToDial; i++ {
		try := r.book.PickAddress(newBias)
		if try == nil {
			continue
		}
		if _, selected := toDial[try.NodeId]; selected {
			continue
		}
		if dialling := r.Adapter.IsDialing(try.NodeId); dialling {
			continue
		}
		if connected := r.Adapter.Peers().Has(try.NodeId); connected {
			continue
		}
		// TODO: consider moving some checks from toDial into here
		// so we don't even consider dialing peers that we want to wait
		// before dialling again, or have dialed too many times already
		r.Logger.Info("Will dial address", "addr", try)
		toDial[try.NodeId] = try
	}

	// Dial picked addresses
	for _, addr := range toDial {
		go r.dialPeer(addr)
	}

	// If we need more addresses, pick a random peer and ask for more.
	if r.book.NeedMoreAddrs() {
		peers := r.Adapter.Peers().List()
		peersCount := len(peers)
		if peersCount > 0 {
			peer := peers[util.RandomInt()%peersCount] // nolint: gas
			r.Logger.Info("We need more addresses. Sending pexRequest to random peer", "peer", peer)
			r.RequestAddrs(peer)
		}
	}

	// If we are not connected to nor dialing anybody, fallback to dialing a seed.
	if out + in + dial + len(toDial) == 0 {
		r.Logger.Info("No addresses to dial nor connected peers. Falling back to seeds")
		r.dialSeeds()
	}
}

func (r *PexReactor) dialAttemptsInfo(addr *p2p.NetAddress) (attempts int, lastDialed time.Time) {
	_attempts, ok := r.attemptsToDial.Load(addr.DialString())
	if !ok {
		return
	}
	atd := _attempts.(_AttemptsToDial)
	return atd.number, atd.lastDialed
}

func (r *PexReactor) dialPeer(addr *p2p.NetAddress) {
	attempts, lastDialed := r.dialAttemptsInfo(addr)

	if attempts > _MAX_ATTEMPTS_TO_DIAL {
		r.Logger.Error("Reached max attempts to dial", "addr", addr, "attempts", attempts)
		r.book.MarkBad(addr)
		return
	}

	// exponential backoff if it's not our first attempt to dial given address
	if attempts > 0 {
		jitterSeconds := time.Duration(util.RandomFloat64() * float64(time.Second)) // 1s == (1e9 ns)
		backoffDuration := jitterSeconds + ((1 << uint(attempts)) * time.Second)
		sinceLastDialed := time.Since(lastDialed)
		if sinceLastDialed < backoffDuration {
			r.Logger.Debug("Too early to dial", "addr", addr, "backoff_duration", backoffDuration, "last_dialed", lastDialed, "time_since", sinceLastDialed)
			return
		}
	}

	err := r.Adapter.DialPeerWithAddress(addr, false)
	if err != nil {
		r.Logger.Error("Dialing failed", "addr", addr, "err", err, "attempts", attempts)
		// TODO: detect more "bad peer" scenarios
		if _, ok := err.(p2p.ErrAdapterAuthenticationFailure); ok {
			r.book.MarkBad(addr)
			r.attemptsToDial.Delete(addr.DialString())
		} else {
			r.book.MarkAttempt(addr)
			// FIXME: if the addr is going to be removed from the addrbook (hard to
			// tell at this point), we need to Remove it from attemptsToDial, not
			// record another attempt.
			// record attempt
			r.attemptsToDial.Store(addr.DialString(), _AttemptsToDial{attempts + 1, time.Now()})
		}
	} else {
		// cleanup any history
		r.attemptsToDial.Delete(addr.DialString())
	}
}

// check seed addresses are well formed
func (r *PexReactor) checkSeeds() error {
	lSeeds := len(r.config.Seeds)
	if lSeeds == 0 {
		return nil
	}
	_, errs := p2p.NewNetAddressStrings(r.config.Seeds)
	for _, err := range errs {
		if err != nil {
			return err
		}
	}
	return nil
}

// randomly dial seeds until we connect to one or exhaust them
func (r *PexReactor) dialSeeds() {
	lSeeds := len(r.config.Seeds)
	if lSeeds == 0 {
		return
	}
	seedAddrs, _ := p2p.NewNetAddressStrings(r.config.Seeds)

	perm := util.MyRand.Perm(lSeeds)
	for _, i := range perm {
		// dial a random seed
		seedAddr := seedAddrs[i]
		err := r.Adapter.DialPeerWithAddress(seedAddr, false)
		if err == nil {
			return
		}
		r.Adapter.Logger.Error("Error dialing seed", "err", err, "seed", seedAddr)
	}
	r.Adapter.Logger.Error("Couldn't connect to any seeds")
}

// AttemptsToDial returns the number of attempts to dial specific address. It
// returns 0 if never attempted or successfully connected.
func (r *PexReactor) AttemptsToDial(addr *p2p.NetAddress) int {
	lAttempts, attempted := r.attemptsToDial.Load(addr.DialString())
	if attempted {
		return lAttempts.(_AttemptsToDial).number
	}
	return 0
}

//----------------------------------------------------------

// Explores the network searching for more peers. (continuous)
// Seed/Crawler Mode causes this node to quickly disconnect
// from peers, except other seed nodes.
func (r *PexReactor) crawlPeersRoutine() {
	r.crawlPeers()

	ticker := time.NewTicker(_CRAWL_PEER_PERIOD)
	for {
		select {
		case <-ticker.C:
			r.attemptDisconnects()
			r.crawlPeers()

		case <-r.C4Quit():
			ticker.Stop()
			return
		}
	}
}

// hasPotentialPeers indicates if there is a potential peer to connect to, by
// consulting the Adapter as well as the AddrBook.
func (r *PexReactor) hasPotentialPeers() bool {
	out, in, dialing := r.Adapter.NumPeers()

	return out + in + dialing > 0 && r.book.CountOfKnownAddress() > 0
}

type _CrawlPeerInfo struct {
	Addr *p2p.NetAddress
	LastAttempt time.Time
	LastSuccess time.Time
}

func (r *PexReactor) getPeersToCrawl(interval time.Duration) []_CrawlPeerInfo {
	var cpis []_CrawlPeerInfo

	now := time.Now()
	r.book.IterateKnownAddresses(func (addr *_KnownAddress) {
		if len(addr.NodeId()) > 0 && now.Sub(addr.LastAttempt) >= interval {
			cpis = append(cpis, _CrawlPeerInfo{
				Addr: addr.Addr,
				LastAttempt: addr.LastAttempt,
				LastSuccess: addr.LastSuccess,
			})
		}
	})

	sort.Slice(cpis, func (i, j int) bool {
		return cpis[i].LastAttempt.Before(cpis[j].LastAttempt)
	})
	return cpis
}

// crawlPeers will crawl the network looking for new peer addresses. (once)
func (r *PexReactor) crawlPeers() {
	cpis := r.getPeersToCrawl(_CRAWL_PEER_INTERVAL)
	for _, pi := range cpis {
		err := r.Adapter.DialPeerWithAddress(pi.Addr, false)
		if err != nil {
			r.book.MarkAttempt(pi.Addr)
			continue
		}

		peer := r.Adapter.Peers().Get(pi.Addr.NodeId)
		if peer != nil {
			r.RequestAddrs(peer)
		}
	}
}

// attemptDisconnects checks if we've been with each peer long enough to disconnect
func (r *PexReactor) attemptDisconnects() {
	for _, peer := range r.Adapter.Peers().List() {
		if peer.ConnStatus().Duration < _SEED_DISCONNECT_WAIT_PERIOD {
			continue
		}
		if peer.IsPersistent() {
			continue
		}
		r.Adapter.StopPeerGracefully(peer)
	}
}

// isAddrPrivate returns true if addr.NodeId is a private NodeId.
func (r *PexReactor) isAddrPrivate(addr *p2p.NetAddress) bool {
	for _, id := range r.config.PrivatePeerIds {
		if string(addr.NodeId) == id {
			return true
		}
	}
	return false
}

