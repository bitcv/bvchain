package p2p

import (
	"bvchain/cfg"
	"bvchain/ec"
	"bvchain/util/log"
	"bvchain/p2p/cxn"

	"bytes"
	"fmt"
	"net"
	"sync"
	"testing"
	"time"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

var _config *cfg.P2pConfig

func init() {
	_config = cfg.DefaultP2pConfig()
	_config.PexReactor = true
	_config.AllowDuplicateIp = true
}

type _PeerMessage struct {
	PeerId  NodeId
	Bytes   []byte
	Counter int
}

type _TestReactor struct {
	BaseReactor

	mtx          sync.Mutex
	channels     []*cxn.ChannelDescriptor
	logMessages  bool
	msgsCounter  int
	msgsReceived map[byte][]_PeerMessage
}

func NewTestReactor(channels []*cxn.ChannelDescriptor, logMessages bool) *_TestReactor {
	tr := &_TestReactor{
		channels:     channels,
		logMessages:  logMessages,
		msgsReceived: make(map[byte][]_PeerMessage),
	}
	tr.BaseReactor.Init("_TestReactor", tr)
	tr.SetLogger(log.TestingLogger())
	return tr
}

func (tr *_TestReactor) GetChannels() []*cxn.ChannelDescriptor {
	return tr.channels
}

func (tr *_TestReactor) AddPeer(peer Peer) {}

func (tr *_TestReactor) RemovePeer(peer Peer, reason interface{}) {}

func (tr *_TestReactor) ReceiveMessage(peer Peer, chId byte, msgBytes []byte) {
	if tr.logMessages {
		tr.mtx.Lock()
		defer tr.mtx.Unlock()
		tr.msgsReceived[chId] = append(tr.msgsReceived[chId], _PeerMessage{peer.NodeId(), msgBytes, tr.msgsCounter})
		tr.msgsCounter++
	}
}

func (tr *_TestReactor) getMsgs(chId byte) []_PeerMessage {
	tr.mtx.Lock()
	defer tr.mtx.Unlock()
	return tr.msgsReceived[chId]
}

//-----------------------------------------------------------------------------

// convenience method for creating two adapters connected to each other.
// XXX: note this uses net.Pipe and not a proper TCP conn
func MakeAdapterPair(t testing.TB, initAdapter func(int, *Adapter) *Adapter) (*Adapter, *Adapter) {
	// Create two adapters that will be interconnected.
	adapters := MakeConnectedAdapters(_config, 2, initAdapter, Connect2Adapters)
	return adapters[0], adapters[1]
}

func initAdapterFunc(i int, ap *Adapter) *Adapter {
	ap.SetAddrBook(&_MockAddrBook{
		addrs:    make(map[string]struct{}),
		ourAddrs: make(map[string]struct{})})

	// Make two reactors of two channels each
	ap.AddReactor("foo", NewTestReactor([]*cxn.ChannelDescriptor{
		{Id: byte(0x00), Priority: 10},
		{Id: byte(0x01), Priority: 10},
	}, true))
	ap.AddReactor("bar", NewTestReactor([]*cxn.ChannelDescriptor{
		{Id: byte(0x02), Priority: 10},
		{Id: byte(0x03), Priority: 10},
	}, true))

	return ap
}

func TestAdapters(t *testing.T) {
	ap1, ap2 := MakeAdapterPair(t, initAdapterFunc)
	defer ap1.Stop()
	defer ap2.Stop()

	if ap1.Peers().Size() != 1 {
		t.Errorf("Expected exactly 1 peer in ap1, got %v", ap1.Peers().Size())
	}
	if ap2.Peers().Size() != 1 {
		t.Errorf("Expected exactly 1 peer in ap2, got %v", ap2.Peers().Size())
	}

	// Lets send some messages
	ch0Msg := []byte("channel zero")
	ch1Msg := []byte("channel foo")
	ch2Msg := []byte("channel bar")

	ap1.Broadcast(byte(0x00), ch0Msg)
	ap1.Broadcast(byte(0x01), ch1Msg)
	ap1.Broadcast(byte(0x02), ch2Msg)

	assertMsgReceivedWithTimeout(t, ch0Msg, byte(0x00), ap2.GetReactor("foo").(*_TestReactor), 10*time.Millisecond, 5*time.Second)
	assertMsgReceivedWithTimeout(t, ch1Msg, byte(0x01), ap2.GetReactor("foo").(*_TestReactor), 10*time.Millisecond, 5*time.Second)
	assertMsgReceivedWithTimeout(t, ch2Msg, byte(0x02), ap2.GetReactor("bar").(*_TestReactor), 10*time.Millisecond, 5*time.Second)
}

func assertMsgReceivedWithTimeout(t *testing.T, msgBytes []byte, channel byte, reactor *_TestReactor, checkPeriod, timeout time.Duration) {
	ticker := time.NewTicker(checkPeriod)
	for {
		select {
		case <-ticker.C:
			msgs := reactor.getMsgs(channel)
			if len(msgs) > 0 {
				if !bytes.Equal(msgs[0].Bytes, msgBytes) {
					t.Fatalf("Unexpected message bytes. Wanted: %X, Got: %X", msgBytes, msgs[0].Bytes)
				}
				return
			}
		case <-time.After(timeout):
			t.Fatalf("Expected to have received 1 message in channel #%v, got zero", channel)
		}
	}
}

func TestConnAddrFilter(t *testing.T) {
	ap1 := MakeAdapter(_config, 1, "testing", "123.123.123", initAdapterFunc)
	ap2 := MakeAdapter(_config, 1, "testing", "123.123.123", initAdapterFunc)
	defer ap1.Stop()
	defer ap2.Stop()

	c1, c2 := net.Pipe()

	ap1.SetAddrFilter(func(addr net.Addr) error {
		if addr.String() == c1.RemoteAddr().String() {
			return fmt.Errorf("Error: pipe is blacklisted")
		}
		return nil
	})

	// connect to good peer
	go func() {
		err := ap1.addPeerWithConnection(c1)
		assert.NotNil(t, err, "expected err")
	}()
	go func() {
		err := ap2.addPeerWithConnection(c2)
		assert.NotNil(t, err, "expected err")
	}()

	assertNoPeersAfterTimeout(t, ap1, 400*time.Millisecond)
	assertNoPeersAfterTimeout(t, ap2, 400*time.Millisecond)
}

func TestAdapterFiltersOutItself(t *testing.T) {
	ap1 := MakeAdapter(_config, 1, "127.0.0.1", "123.123.123", initAdapterFunc)
	// addr := ap1.NodeInfo().NetAddress()

	// // add ourselves like we do in node.go#427
	// ap1.addrBook.AddOurAddress(addr)

	// simulate ap1 having a public IP by creating a remote peer with the same Id
	rp := &_RemotePeer{PrivKey: ap1.nodeKey.PrivKey, Config: _config}
	rp.Start()

	// addr should be rejected in addPeer based on the same Id
	err := ap1.DialPeerWithAddress(rp.Addr(), false)
	if assert.Error(t, err) {
		assert.Equal(t, ErrAdapterConnectToSelf{rp.Addr()}.Error(), err.Error())
	}

	assert.True(t, ap1.addrBook.IsOurAddress(rp.Addr()))

	assert.False(t, ap1.addrBook.HasAddress(rp.Addr()))

	rp.Stop()

	assertNoPeersAfterTimeout(t, ap1, 100*time.Millisecond)
}

func assertNoPeersAfterTimeout(t *testing.T, ap *Adapter, timeout time.Duration) {
	time.Sleep(timeout)
	if ap.Peers().Size() != 0 {
		t.Fatalf("Expected %v to not connect to some peers, got %d", ap, ap.Peers().Size())
	}
}

func TestConnIdFilter(t *testing.T) {
	ap1 := MakeAdapter(_config, 1, "testing", "123.123.123", initAdapterFunc)
	ap2 := MakeAdapter(_config, 1, "testing", "123.123.123", initAdapterFunc)
	defer ap1.Stop()
	defer ap2.Stop()

	c1, c2 := net.Pipe()

	ap1.SetIdFilter(func(id NodeId) error {
		if id == ap2.nodeInfo.NodeId {
			return fmt.Errorf("Error: pipe is blacklisted")
		}
		return nil
	})

	ap2.SetIdFilter(func(id NodeId) error {
		if id == ap1.nodeInfo.NodeId {
			return fmt.Errorf("Error: pipe is blacklisted")
		}
		return nil
	})

	go func() {
		err := ap1.addPeerWithConnection(c1)
		assert.NotNil(t, err, "expected error")
	}()
	go func() {
		err := ap2.addPeerWithConnection(c2)
		assert.NotNil(t, err, "expected error")
	}()

	assertNoPeersAfterTimeout(t, ap1, 400*time.Millisecond)
	assertNoPeersAfterTimeout(t, ap2, 400*time.Millisecond)
}

func TestAdapterStopsNonPersistentPeerOnError(t *testing.T) {
	assert, require := assert.New(t), require.New(t)

	ap := MakeAdapter(_config, 1, "testing", "123.123.123", initAdapterFunc)
	err := ap.Start()
	if err != nil {
		t.Error(err)
	}
	defer ap.Stop()

	// simulate remote peer
	rp := &_RemotePeer{PrivKey: ec.NewPrivKey(), Config: _config}
	rp.Start()
	defer rp.Stop()

	pc, err := newConnectionOutgoing(_config, ap.nodeKey.PrivKey, rp.Addr(), false) 
	require.Nil(err)
	err = ap.addPeerConn(pc)
	require.Nil(err)

	peer := ap.Peers().Get(rp.NodeId())
	require.NotNil(peer)

	// simulate failure by closing connection
	pc.CloseConn()

	assertNoPeersAfterTimeout(t, ap, 100*time.Millisecond)
	assert.False(peer.IsRunning())
}

func TestAdapterReconnectsToPersistentPeer(t *testing.T) {
	assert, require := assert.New(t), require.New(t)

	ap := MakeAdapter(_config, 1, "testing", "123.123.123", initAdapterFunc)
	err := ap.Start()
	if err != nil {
		t.Error(err)
	}
	defer ap.Stop()

	// simulate remote peer
	rp := &_RemotePeer{PrivKey: ec.NewPrivKey(), Config: _config}
	rp.Start()
	defer rp.Stop()

	pc, err := newConnectionOutgoing(_config, ap.nodeKey.PrivKey, rp.Addr(), true) 
	//	ap.reactorsByCh, ap.chDescs, ap.StopPeerForError, ap.nodeKey.PrivKey,
	require.Nil(err)

	require.Nil(ap.addPeerConn(pc))

	peer := ap.Peers().Get(rp.NodeId())
	require.NotNil(peer)

	// simulate failure by closing connection
	pc.CloseConn()

	// TODO: remove sleep, detect the disconnection, wait for reconnect
	npeers := ap.Peers().Size()
	for i := 0; i < 20; i++ {
		time.Sleep(250 * time.Millisecond)
		npeers = ap.Peers().Size()
		if npeers > 0 {
			break
		}
	}
	assert.NotZero(npeers)
	assert.False(peer.IsRunning())

	// simulate another remote peer
	rp = &_RemotePeer{
		PrivKey: ec.NewPrivKey(),
		Config:  _config,
		// Use different interface to prevent duplicate IP filter, this will break
		// beyond two peers.
		listenAddr: "127.0.0.1:0",
	}
	rp.Start()
	defer rp.Stop()

	// simulate first time dial failure
	conf := cfg.DefaultP2pConfig()
	conf.TestDialFail = true
	err = ap.addOutgoingPeerWithConfig(rp.Addr(), conf, true)
	require.NotNil(err)

	// DialPeerWithAddres - ap.peerConfig resets the dialer

	// TODO: same as above
	for i := 0; i < 20; i++ {
		time.Sleep(250 * time.Millisecond)
		npeers = ap.Peers().Size()
		if npeers > 1 {
			break
		}
	}
	assert.EqualValues(2, npeers)
}

func TestAdapterFullConnectivity(t *testing.T) {
	adapters := MakeConnectedAdapters(_config, 3, initAdapterFunc, Connect2Adapters)
	defer func() {
		for _, ap := range adapters {
			ap.Stop()
		}
	}()

	for i, ap := range adapters {
		if ap.Peers().Size() != 2 {
			t.Fatalf("Expected each switch to be connected to 2 other, but %d switch only connected to %d", ap.Peers().Size(), i)
		}
	}
}

func BenchmarkAdapterBroadcast(b *testing.B) {
	ap1, ap2 := MakeAdapterPair(b, func(i int, ap *Adapter) *Adapter {
		ap.AddReactor("foo", NewTestReactor([]*cxn.ChannelDescriptor{
			{Id: byte(0x00), Priority: 10},
			{Id: byte(0x01), Priority: 10},
		}, false))
		ap.AddReactor("bar", NewTestReactor([]*cxn.ChannelDescriptor{
			{Id: byte(0x02), Priority: 10},
			{Id: byte(0x03), Priority: 10},
		}, false))
		return ap
	})
	defer ap1.Stop()
	defer ap2.Stop()

	// Allow time for goroutines to boot up
	time.Sleep(1 * time.Second)

	b.ResetTimer()

	numSuccess, numFailure := 0, 0

	// Send random message from foo channel to another
	for i := 0; i < b.N; i++ {
		chId := byte(i % 4)
		successChan := ap1.Broadcast(chId, []byte("test data"))
		for s := range successChan {
			if s {
				numSuccess++
			} else {
				numFailure++
			}
		}
	}

	b.Logf("success: %v, failure: %v", numSuccess, numFailure)
}

type _MockAddrBook struct {
	addrs    map[string]struct{}
	ourAddrs map[string]struct{}
}

var _ AddrBook = (*_MockAddrBook)(nil)

func (book *_MockAddrBook) AddAddress(addr *NetAddress, src *NetAddress) error {
	book.addrs[addr.String()] = struct{}{}
	return nil
}
func (book *_MockAddrBook) AddOurAddress(addr *NetAddress) { book.ourAddrs[addr.String()] = struct{}{} }
func (book *_MockAddrBook) IsOurAddress(addr *NetAddress) bool {
	_, ok := book.ourAddrs[addr.String()]
	return ok
}
func (book *_MockAddrBook) MarkGood(*NetAddress) {}
func (book *_MockAddrBook) HasAddress(addr *NetAddress) bool {
	_, ok := book.addrs[addr.String()]
	return ok
}
func (book *_MockAddrBook) RemoveAddress(addr *NetAddress) {
	delete(book.addrs, addr.String())
}

func (book *_MockAddrBook) Save() {}

