package pex

import (
	"bvchain/p2p"
	"bvchain/p2p/cxn"
	"bvchain/cfg"
	"bvchain/util/log"
	"bvchain/util"
	"bvchain/ec"

	"fmt"
	"io/ioutil"
	"net"
	"os"
	"path/filepath"
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

func TestPexReactorBasic(t *testing.T) {
	r, book := createReactor(&PexReactorConfig{})
	defer teardownReactor(book)

	assert.NotNil(t, r)
	assert.NotEmpty(t, r.GetChannels())
}

func TestPexReactorAddRemovePeer(t *testing.T) {
	r, book := createReactor(&PexReactorConfig{})
	defer teardownReactor(book)

	size := book.Size()
	peer := p2p.CreateRandomPeer(false)

	r.AddPeer(peer)
	assert.Equal(t, size+1, book.Size())

	r.RemovePeer(peer, "peer not available")

	outgoingPeer := p2p.CreateRandomPeer(true)

	r.AddPeer(outgoingPeer)
	assert.Equal(t, size+1, book.Size(), "outgoing peers should not be added to the address book")

	r.RemovePeer(outgoingPeer, "peer not available")
}

// --- FAIL: TestPexReactorRunning (10.01s)
// 				pex_reactor_test.go:412: expected all adapters to be connected to at
// 				least one peer (adapters: 0 => {outgoing: 1, incoming: 0}, 1 =>
// 				{outgoing: 0, incoming: 1}, 2 => {outgoing: 0, incoming: 0}, )
//
// EXPLANATION: peers are getting rejected because in adapter#addPeer we check
// if any peer (who we already connected to) has the same IP. Even though local
// peers have different IP addresses, they all have the same underlying remote
// IP: 127.0.0.1.
//
func TestPexReactorRunning(t *testing.T) {
	N := 3
	adapters := make([]*p2p.Adapter, N)

	// directory to store address books
	dir, err := ioutil.TempDir("", "pex_reactor")
	require.Nil(t, err)
	defer os.RemoveAll(dir) // nolint: errcheck

	books := make([]*_AddrBook, N)
	logger := log.TestingLogger()

	// create adapters
	for i := 0; i < N; i++ {
		adapters[i] = p2p.MakeAdapter(_config, i, "testing", "123.123.123", func(i int, ap *p2p.Adapter) *p2p.Adapter {
			books[i] = NewAddrBook(filepath.Join(dir, fmt.Sprintf("addrbook%d.json", i)), false)
			books[i].SetLogger(logger.With("pex", i))
			ap.SetAddrBook(books[i])

			ap.SetLogger(logger.With("pex", i))

			r := NewPexReactor(books[i], &PexReactorConfig{})
			r.SetLogger(logger.With("pex", i))
			r.SetEnsurePeersPeriod(250 * time.Millisecond)
			ap.AddReactor("pex", r)

			return ap
		})
	}

	addOtherToAddrBook := func(me, other int) {
		addr := adapters[other].NodeInfo().NetAddress()
		books[me].AddAddress(addr, addr)
	}

	addOtherToAddrBook(0, 1)
	addOtherToAddrBook(1, 0)
	addOtherToAddrBook(2, 1)

	for i, ap := range adapters {
		ap.AddListener(p2p.NewListener("tcp://"+ap.NodeInfo().ListenAddr, "", false, logger.With("pex", i)))
		err := ap.Start()
		require.Nil(t, err)
	}

	assertPeersWithTimeout(t, adapters, 10*time.Millisecond, 10*time.Second, N-1)

	for _, ap := range adapters {
		ap.Stop()
	}
}

func TestPexReactorReceive(t *testing.T) {
	r, book := createReactor(&PexReactorConfig{})
	defer teardownReactor(book)

	peer := p2p.CreateRandomPeer(false)

	// we have to send a request to receive responses
	r.RequestAddrs(peer)

	size := book.Size()
	addrs := []*p2p.NetAddress{peer.NodeInfo().NetAddress()}
	msg := p2p.TheMK.MustMarshal(p2p.PEX_CHANNEL, &_PexAddrsMessage{Addrs:addrs})
	r.ReceiveMessage(peer, p2p.PEX_CHANNEL, msg)
	assert.Equal(t, size+1, book.Size())

	msg = p2p.TheMK.MustMarshal(p2p.PEX_CHANNEL, &_PexRequestMessage{})
	r.ReceiveMessage(peer, p2p.PEX_CHANNEL, msg) // should not panic.
}

func TestPexReactorRequestMessageAbuse(t *testing.T) {
	r, book := createReactor(&PexReactorConfig{})
	defer teardownReactor(book)

	ap := createAdapterAndAddReactors(r)
	ap.SetAddrBook(book)

	peer := newMockPeer()
	p2p.AddPeerToAdapter(ap, peer)
	assert.True(t, ap.Peers().Has(peer.NodeId()))

	id := string(peer.NodeId())
	msg := p2p.TheMK.MustMarshal(p2p.PEX_CHANNEL, &_PexRequestMessage{})

	// first time creates the entry
	r.ReceiveMessage(peer, p2p.PEX_CHANNEL, msg)
	assert.True(t, r.lastReceivedRequests.Has(id))
	assert.True(t, ap.Peers().Has(peer.NodeId()))

	// next time sets the last time value
	r.ReceiveMessage(peer, p2p.PEX_CHANNEL, msg)
	assert.True(t, r.lastReceivedRequests.Has(id))
	assert.True(t, ap.Peers().Has(peer.NodeId()))

	// third time is too many too soon - peer is removed
	r.ReceiveMessage(peer, p2p.PEX_CHANNEL, msg)
	assert.False(t, r.lastReceivedRequests.Has(id))
	assert.False(t, ap.Peers().Has(peer.NodeId()))
}

func TestPexReactorAddrsMessageAbuse(t *testing.T) {
	r, book := createReactor(&PexReactorConfig{})
	defer teardownReactor(book)

	ap := createAdapterAndAddReactors(r)
	ap.SetAddrBook(book)

	peer := newMockPeer()
	p2p.AddPeerToAdapter(ap, peer)
	assert.True(t, ap.Peers().Has(peer.NodeId()))

	id := string(peer.NodeId())

	// request addrs from the peer
	r.RequestAddrs(peer)
	assert.True(t, r.requestsSent.Has(id))
	assert.True(t, ap.Peers().Has(peer.NodeId()))

	addrs := []*p2p.NetAddress{peer.NodeInfo().NetAddress()}
	msg := p2p.TheMK.MustMarshal(p2p.PEX_CHANNEL, &_PexAddrsMessage{Addrs:addrs})

	// receive some addrs. should clear the request
	r.ReceiveMessage(peer, p2p.PEX_CHANNEL, msg)
	assert.False(t, r.requestsSent.Has(id))
	assert.True(t, ap.Peers().Has(peer.NodeId()))

	// receiving more addrs causes a disconnect
	r.ReceiveMessage(peer, p2p.PEX_CHANNEL, msg)
	assert.False(t, ap.Peers().Has(peer.NodeId()))
}

func TestPexReactorUsesSeedsIfNeeded(t *testing.T) {
	// directory to store address books
	dir, err := ioutil.TempDir("", "pex_reactor")
	require.Nil(t, err)
	defer os.RemoveAll(dir) // nolint: errcheck

	// 1. create seed
	seed := p2p.MakeAdapter(
		_config,
		0,
		"127.0.0.1",
		"123.123.123",
		func(i int, ap *p2p.Adapter) *p2p.Adapter {
			book := NewAddrBook(filepath.Join(dir, "addrbook0.json"), false)
			book.SetLogger(log.TestingLogger())
			ap.SetAddrBook(book)

			ap.SetLogger(log.TestingLogger())

			r := NewPexReactor(book, &PexReactorConfig{})
			r.SetLogger(log.TestingLogger())
			ap.AddReactor("pex", r)
			return ap
		},
	)
	seed.AddListener(
		p2p.NewListener("tcp://"+seed.NodeInfo().ListenAddr, "", false, log.TestingLogger()),
	)
	require.Nil(t, seed.Start())
	defer seed.Stop()

	// 2. create usual peer with only seed configured.
	peer := p2p.MakeAdapter(
		_config,
		1,
		"127.0.0.1",
		"123.123.123",
		func(i int, ap *p2p.Adapter) *p2p.Adapter {
			book := NewAddrBook(filepath.Join(dir, "addrbook1.json"), false)
			book.SetLogger(log.TestingLogger())
			ap.SetAddrBook(book)

			ap.SetLogger(log.TestingLogger())

			r := NewPexReactor(
				book,
				&PexReactorConfig{
					Seeds: []string{seed.NodeInfo().NetAddress().String()},
				},
			)
			r.SetLogger(log.TestingLogger())
			ap.AddReactor("pex", r)
			return ap
		},
	)
	require.Nil(t, peer.Start())
	defer peer.Stop()

	// 3. check that the peer connects to seed immediately
	assertPeersWithTimeout(t, []*p2p.Adapter{peer}, 10*time.Millisecond, 3*time.Second, 1)
}

func TestPexReactorCrawlStatus(t *testing.T) {
	pexR, book := createReactor(&PexReactorConfig{SeedMode: true})
	defer teardownReactor(book)

	// Seed/Crawler mode uses data from the Adapter
	ap := createAdapterAndAddReactors(pexR)
	ap.SetAddrBook(book)

	// Create a peer, add it to the peer set and the addrbook.
	peer := p2p.CreateRandomPeer(false)
	p2p.AddPeerToAdapter(pexR.Adapter, peer)
	addr1 := peer.NodeInfo().NetAddress()
	pexR.book.AddAddress(addr1, addr1)

	// Add a non-connected address to the book.
	_, addr2 := p2p.CreateRoutableAddr()
	pexR.book.AddAddress(addr2, addr1)

	// Get some peerInfos to crawl
	peerInfos := pexR.getPeersToCrawl(time.Duration(0))

	// Make sure it has the proper number of elements
	assert.Equal(t, 2, len(peerInfos))

	// TODO: test
}

func TestPexReactorDoesNotAddPrivatePeersToAddrBook(t *testing.T) {
	peer := p2p.CreateRandomPeer(false)

	pexR, book := createReactor(&PexReactorConfig{PrivatePeerIds: []string{string(peer.NodeInfo().NodeId)}})
	defer teardownReactor(book)

	// we have to send a request to receive responses
	pexR.RequestAddrs(peer)

	size := book.Size()
	addrs := []*p2p.NetAddress{peer.NodeInfo().NetAddress()}
	msg := p2p.TheMK.MustMarshal(p2p.PEX_CHANNEL, &_PexAddrsMessage{Addrs:addrs})
	pexR.ReceiveMessage(peer, p2p.PEX_CHANNEL, msg)
	assert.Equal(t, size, book.Size())

	pexR.AddPeer(peer)
	assert.Equal(t, size, book.Size())
}

func TestPexReactorDialPeer(t *testing.T) {
	pexR, book := createReactor(&PexReactorConfig{})
	defer teardownReactor(book)

	ap := createAdapterAndAddReactors(pexR)
	ap.SetAddrBook(book)

	peer := newMockPeer()
	addr := peer.NodeInfo().NetAddress()

	assert.Equal(t, 0, pexR.AttemptsToDial(addr))

	// 1st unsuccessful attempt
	pexR.dialPeer(addr)

	assert.Equal(t, 1, pexR.AttemptsToDial(addr))

	// 2nd unsuccessful attempt
	pexR.dialPeer(addr)

	// must be skipped because it is too early
	assert.Equal(t, 1, pexR.AttemptsToDial(addr))

	if !testing.Short() {
		time.Sleep(3 * time.Second)

		// 3rd attempt
		pexR.dialPeer(addr)

		assert.Equal(t, 2, pexR.AttemptsToDial(addr))
	}
}

type _MockPeer struct {
	util.BaseService
	pubKey ec.PubKey
	addr  *p2p.NetAddress
	outgoing bool
	persistent bool
}

func newMockPeer() *_MockPeer {
	_, netAddr := p2p.CreateRoutableAddr()
	key := ec.NewPrivKey()
	mp := &_MockPeer{
		addr:   netAddr,
		pubKey: key.PubKey(),
	}
	mp.BaseService.Init(nil, "MockPeer", mp)
	mp.Start()
	return mp
}

func (mp *_MockPeer) NodeId() p2p.NodeId { return mp.addr.NodeId }
func (mp *_MockPeer) IsOutgoing() bool   { return mp.outgoing }
func (mp *_MockPeer) IsPersistent() bool { return mp.persistent }
func (mp *_MockPeer) NodeInfo() *p2p.NodeInfo {
	return &p2p.NodeInfo{
		NodeId:     mp.addr.NodeId,
		ListenAddr: mp.addr.DialString(),
	}
}
func (mp *_MockPeer) RemoteIp() net.IP              { return net.ParseIP("127.0.0.1") }
func (mp *_MockPeer) ConnStatus() *cxn.ConnStatus   { return &cxn.ConnStatus{} }
func (mp *_MockPeer) Send(byte, []byte) bool        { return false }
func (mp *_MockPeer) TrySend(byte, []byte) bool     { return false }
func (mp *_MockPeer) Set(string, interface{})       {}
func (mp *_MockPeer) Get(string) interface{}        { return nil }

func assertPeersWithTimeout(
	t *testing.T,
	adapters []*p2p.Adapter,
	checkPeriod, timeout time.Duration,
	nPeers int,
) {
	ticker := time.NewTicker(checkPeriod)
	remaining := timeout

	for {
		select {
		case <-ticker.C:
			// check peers are connected
			allGood := true
			for _, ap := range adapters {
				outgoing, incoming, _ := ap.NumPeers()
				if outgoing+incoming < nPeers {
					allGood = false
				}
			}
			remaining -= checkPeriod
			if remaining < 0 {
				remaining = 0
			}
			if allGood {
				return
			}
		case <-time.After(remaining):
			numPeersStr := ""
			for i, ap := range adapters {
				outgoing, incoming, _ := ap.NumPeers()
				numPeersStr += fmt.Sprintf("%d => {outgoing: %d, incoming: %d}, ", i, outgoing, incoming)
			}
			t.Errorf(
				"expected all adapters to be connected to at least one peer (adapters: %s)",
				numPeersStr,
			)
			return
		}
	}
}

func createReactor(conf *PexReactorConfig) (r *PexReactor, book *_AddrBook) {
	// directory to store address book
	dir, err := ioutil.TempDir("", "pex_reactor")
	if err != nil {
		panic(err)
	}
	book = NewAddrBook(filepath.Join(dir, "addrbook.json"), true)
	book.SetLogger(log.TestingLogger())

	r = NewPexReactor(book, conf)
	r.SetLogger(log.TestingLogger())
	return
}

func teardownReactor(book *_AddrBook) {
	err := os.RemoveAll(filepath.Dir(book.FilePath()))
	if err != nil {
		panic(err)
	}
}

func createAdapterAndAddReactors(reactors ...p2p.Reactor) *p2p.Adapter {
	ap := p2p.MakeAdapter(_config, 0, "127.0.0.1", "123.123.123", func(i int, ap *p2p.Adapter) *p2p.Adapter { return ap })
	ap.SetLogger(log.TestingLogger())
	for _, r := range reactors {
		ap.AddReactor(r.String(), r)
		r.SetAdapter(ap)
	}
	return ap
}
