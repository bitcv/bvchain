package block

import (
	"bvchain/util"
	"bvchain/util/log"
	"bvchain/cfg"
	"bvchain/p2p"
	"bvchain/state"
	"bvchain/tx"
	"bvchain/db"
	"bvchain/ec"
	"bvchain/trie"

	"net"
	"testing"
	"halftwo/mangos/xerr"
)

func makeStateAndBlockStore(logger log.Logger) (*state.ChainState, *BlockStore) {
	config := cfg.ResetTestRoot("block_reactor_test")
	blockDb := db.NewMemDb()
	stateDb, err := trie.NewWithCacheDb(ec.Hash256{}, trie.NewCacheDb(db.NewMemDb()))
	blockStore := NewBlockStore(blockDb)
	cst, err := state.LoadChainStateFromDbOrGenesisFile(stateDb, config.GenesisFile)
	if err != nil {
		panic(xerr.Trace(err, "error constructing state from genesis file"))
	}
	return cst, blockStore
}

func newBlockReactor(logger log.Logger, maxBlockHeight int64) *BlockReactor {
	st, blockStore := makeStateAndBlockStore(logger)

	fastSync := true
	br := NewBlockReactor(st.Clone(), blockStore, fastSync)
	br.SetLogger(logger.With("module", "block"))

	// Next: we need to set a switch in order for peers to be added in
	br.Adapter = p2p.NewAdapter(cfg.DefaultP2pConfig())

	// Lastly: let's add some blocks in
	for blockHeight := int64(1); blockHeight <= maxBlockHeight; blockHeight++ {
		firstBlock := makeBlock(blockHeight, st)
		blockStore.SaveBlock(firstBlock)
	}

	return br
}

func TestNoBlockResponse(t *testing.T) {
	maxBlockHeight := int64(20)

	br := newBlockReactor(log.TestingLogger(), maxBlockHeight)
	br.Start()
	defer br.Stop()

	// Add some peers in
	peer := newBrTestPeer(p2p.RandomNodeId())
	br.AddPeer(peer)

	chId := byte(p2p.BLOCK_CHANNEL)

	tests := []struct {
		height   int64
		existent bool
	}{
		{maxBlockHeight + 2, false},
		{10, true},
		{1, true},
		{100, false},
	}

	// receive a request message from peer,
	// wait for our response to be received on the peer
	for _, tt := range tests {
		reqMsg := &_BlockRequestMessage{tt.height}
		reqBz := p2p.TheMK.MustMarshal(p2p.BLOCK_CHANNEL, reqMsg)
		br.ReceiveMessage(peer, chId, reqBz)
		msg := peer.lastBlockchainMessage()

		if tt.existent {
			if blockMsg, ok := msg.(*_BlockResponseMessage); !ok {
				t.Fatalf("Expected to receive a block response for height %d", tt.height)
			} else if blockMsg.Block.Height != tt.height {
				t.Fatalf("Expected response to be for height %d, got %d", tt.height, blockMsg.Block.Height)
			}
		} else {
			if noBlockMsg, ok := msg.(*_NoBlockResponseMessage); !ok {
				t.Fatalf("Expected to receive a no block response for height %d", tt.height)
			} else if noBlockMsg.Height != tt.height {
				t.Fatalf("Expected response to be for height %d, got %d", tt.height, noBlockMsg.Height)
			}
		}
	}
}

/*
// NOTE: This is too hard to test without
// an easy way to add test peer to switch
// or without significant refactoring of the module.
// Alternatively we could actually dial a TCP conn but
// that seems extreme.
func TestBadBlockStopsPeer(t *testing.T) {
	maxBlockHeight := int64(20)

	br := newBlockReactor(log.TestingLogger(), maxBlockHeight)
	br.Start()
	defer br.Stop()

	// Add some peers in
	peer := newBrTestPeer(p2p.RandomNodeId())

	// XXX: This doesn't add the peer to anything,
	// so it's hard to check that it's later removed
	br.AddPeer(peer)
	assert.True(t, br.Switch.Peers().Size() > 0)

	// send a bad block from the peer
	// default blocks already dont have commits, so should fail
	block := br.store.LoadBlock(3)
	msg := &_BlockResponseMessage{Block: block}
	peer.Send(BlockchainChannel, struct{ BlockchainMessage }{msg})

	ticker := time.NewTicker(time.Millisecond * 10)
	timer := time.NewTimer(time.Second * 2)
LOOP:
	for {
		select {
		case <-ticker.C:
			if br.Switch.Peers().Size() == 0 {
				break LOOP
			}
		case <-timer.C:
			t.Fatal("Timed out waiting to disconnect peer")
		}
	}
}
*/

//----------------------------------------------
// utility funcs

func makeTrxs(height int64) (trxs []tx.Trx) {
	for i := 0; i < 10; i++ {
		trxs = append(trxs, tx.Trx([]byte{byte(height), byte(i)}))
	}
	return trxs
}

func makeBlock(height int64, st *state.State) *Block {
	block := &Block{}
	return block
}

// The Test peer
type _BrTestPeer struct {
	util.BaseService
	id p2p.NodeId
	ch chan interface{}
}

var _ p2p.Peer = (*_BrTestPeer)(nil)

func newBrTestPeer(id p2p.NodeId) *_BrTestPeer {
	tp := &_BrTestPeer{
		id: id,
		ch: make(chan interface{}, 2),
	}
	tp.BaseService.Init(nil, "_BrTestPeer", tp)
	return tp
}

func (tp *_BrTestPeer) lastBlockchainMessage() interface{} { return <-tp.ch }

func (tp *_BrTestPeer) TrySend(chId byte, msgBytes []byte) bool {
	msg, err := p2p.TheMK.Unmarshal(chId, msgBytes)
	if err != nil {
		panic(xerr.Trace(err, "Error while trying to parse a BlockchainMessage"))
	}
	if _, ok := msg.(*_StatusResponseMessage); ok {
		// Discard status response messages since they skew our results
		// We only want to deal with:
		// + _BlockResponseMessage
		// + _NoBlockResponseMessage
	} else {
		tp.ch <- msg
	}
	return true
}

func (tp *_BrTestPeer) Send(chId byte, msgBytes []byte) bool { return tp.TrySend(chId, msgBytes) }
func (tp *_BrTestPeer) NodeInfo() *p2p.NodeInfo              { return &p2p.NodeInfo{} }
func (tp *_BrTestPeer) ConnStatus() *p2p.ConnStatus          { return &p2p.ConnStatus{} }
func (tp *_BrTestPeer) NodeId() p2p.NodeId                   { return tp.id }
func (tp *_BrTestPeer) IsOutgoing() bool                     { return false }
func (tp *_BrTestPeer) IsPersistent() bool                   { return true }
func (tp *_BrTestPeer) Get(s string) interface{}             { return s }
func (tp *_BrTestPeer) Set(string, interface{})              {}
func (tp *_BrTestPeer) RemoteIp() net.IP                     { return []byte{127, 0, 0, 1} }
func (tp *_BrTestPeer) OriginalAddr() *p2p.NetAddress        { return nil }

