package block

import (
	"bvchain/util"
	"bvchain/util/log"
	"bvchain/p2p"

	"testing"
	"time"
)

func init() {
	_peerTimeout = 2 * time.Second
}

type _TestPeer struct {
	id     p2p.NodeId
	height int64
}

func makePeers(numPeers int, minHeight, maxHeight int64) map[p2p.NodeId]_TestPeer {
	peers := make(map[p2p.NodeId]_TestPeer, numPeers)
	for i := 0; i < numPeers; i++ {
		peerId := p2p.RandomNodeId()
		height := minHeight + util.RandomInt64n(maxHeight-minHeight)
		peers[peerId] = _TestPeer{peerId, height}
	}
	return peers
}

func TestBasic(t *testing.T) {
	start := int64(42)
	peers := makePeers(10, start+1, 1000)
	c4error := make(chan _PeerError, 1000)
	c4request := make(chan _BlockRequest, 1000)
	pool := NewBlockPool(start, c4request, c4error)
	pool.SetLogger(log.TestingLogger())

	err := pool.Start()
	if err != nil {
		t.Error(err)
	}

	defer pool.Stop()

	// Introduce each peer.
	go func() {
		for _, peer := range peers {
			pool.SetPeerHeight(peer.id, peer.height)
		}
	}()

	// Start a goroutine to pull blocks
	go func() {
		for {
			if !pool.IsRunning() {
				return
			}
			first, second := pool.PeekTwoBlocks()
			if first != nil && second != nil {
				pool.PopRequest()
			} else {
				time.Sleep(1 * time.Second)
			}
		}
	}()

	// Pull from channels
	for {
		select {
		case err := <-c4error:
			t.Error(err)
		case request := <-c4request:
			t.Logf("Pulled new _BlockRequest %v", request)
			if request.Height == 300 {
				return // Done!
			}
			// Request desired, pretend like we got the block immediately.
			go func() {
				block := &Block{Header: Header{Height: request.Height}}
				pool.AddBlock(request.PeerId, block, 123)
				t.Logf("Added block from peer %v (height: %v)", request.PeerId, request.Height)
			}()
		}
	}
}

func TestTimeout(t *testing.T) {
	start := int64(42)
	peers := makePeers(10, start+1, 1000)
	c4error := make(chan _PeerError, 1000)
	c4request := make(chan _BlockRequest, 1000)
	pool := NewBlockPool(start, c4request, c4error)
	pool.SetLogger(log.TestingLogger())
	err := pool.Start()
	if err != nil {
		t.Error(err)
	}
	defer pool.Stop()

	for _, peer := range peers {
		t.Logf("Peer %v", peer.id)
	}

	// Introduce each peer.
	go func() {
		for _, peer := range peers {
			pool.SetPeerHeight(peer.id, peer.height)
		}
	}()

	// Start a goroutine to pull blocks
	go func() {
		for {
			if !pool.IsRunning() {
				return
			}
			first, second := pool.PeekTwoBlocks()
			if first != nil && second != nil {
				pool.PopRequest()
			} else {
				time.Sleep(1 * time.Second)
			}
		}
	}()

	// Pull from channels
	counter := 0
	timedOut := map[p2p.NodeId]struct{}{}
	for {
		select {
		case err := <-c4error:
			t.Log(err)
			// consider error to be always timeout here
			if _, ok := timedOut[err.peerId]; !ok {
				counter++
				if counter == len(peers) {
					return // Done!
				}
			}
		case request := <-c4request:
			t.Logf("Pulled new _BlockRequest %+v", request)
		}
	}
}
