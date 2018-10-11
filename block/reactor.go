package block

import (
	"bvchain/util/log"
	"bvchain/p2p"
	"bvchain/state"

	"fmt"
	"reflect"
	"time"
)


const _TRY_SYNC_INTERVAL = 10 * time.Millisecond

// ask for best height every 10s
const _STATUS_UPDATE_INTERVAL = 10 * time.Second

// check if we should switch to consensus reactor
const _SWITCH_TO_CONSENSUS_INTERVAL = 1 * time.Second

const _MAX_MSG_SIZE  = 1024*1024*10

type _ConsensusReactor interface {
	// for when we switch from blockchain reactor and fast sync to
	// the consensus machine
	SwitchToConsensus(*state.ChainState, int)
}

// BlockReactor handles long-term catchup syncing.
type BlockReactor struct {
	p2p.BaseReactor

	// immutable
	initialState state.ChainState

	bexec *BlockExecutor
	bstore *BlockStore
	pool  *BlockPool
	fastSync  bool

	c4request <-chan _BlockRequest
	c4error   <-chan _PeerError
}

// NewBlockReactor returns new reactor instance.
func NewBlockReactor(cst *state.ChainState, bexec *BlockExecutor, bstore *BlockStore, fastSync bool) *BlockReactor {

	if cst.LastBlockHeight != bstore.Height() {
		panic(fmt.Sprintf("ChainState (%v) and BlockStore (%v) height mismatch", cst.LastBlockHeight,
			bstore.Height()))
	}

	c4request := make(chan _BlockRequest, _MAX_TOTAL_REQUESTERS)

	const capacity = 1000                      // must be bigger than peers count
	c4error := make(chan _PeerError, capacity) // so we don't block in #Receive#pool.AddBlock

	pool := NewBlockPool(
		bstore.Height()+1,
		c4request,
		c4error,
	)

	br := &BlockReactor{
		initialState: *cst,
		bexec: bexec,
		bstore: bstore,
		pool: pool,
		fastSync: fastSync,
		c4request: c4request,
		c4error: c4error,
	}
	br.BaseReactor.Init("BlockReactor", br)
	return br
}

// SetLogger implements util.Service by setting the logger on reactor and pool.
func (br *BlockReactor) SetLogger(l log.Logger) {
	br.BaseService.Logger = l
	br.pool.Logger = l
}

// OnStart implements util.Service.
func (br *BlockReactor) OnStart() error {
	if br.fastSync {
		err := br.pool.Start()
		if err != nil {
			return err
		}
		go br.poolRoutine()
	}
	return nil
}

// OnStop implements util.Service.
func (br *BlockReactor) OnStop() {
	br.pool.Stop()
}

// GetChannels implements Reactor
func (br *BlockReactor) GetChannels() []*p2p.ChannelDescriptor {
	return []*p2p.ChannelDescriptor{
		{
			Id:                  p2p.BLOCK_CHANNEL,
			Priority:            10,
			SendQueueCapacity:   1000,
			RecvBufferCapacity:  50 * 4096,
			RecvMessageCapacity: _MAX_MSG_SIZE,
		},
	}
}

// AddPeer implements Reactor by sending our state to peer.
func (br *BlockReactor) AddPeer(peer p2p.Peer) {
	height := br.bstore.Height()
	msgBytes := p2p.TheMK.MustMarshal(p2p.BLOCK_CHANNEL, &_StatusInfoMessage{Height:height, Hash:br.bstore.LoadHash(height)})
	if !peer.Send(p2p.BLOCK_CHANNEL, msgBytes) {
		// doing nothing, will try later in `poolRoutine`
	}
	// peer is added to the pool once we receive the first
	// _StatusInfoMessage from the peer and call pool.SetPeerHeight
}

// RemovePeer implements Reactor by removing peer from the pool.
func (br *BlockReactor) RemovePeer(peer p2p.Peer, reason interface{}) {
	br.pool.RemovePeer(peer.NodeId())
}

// respondToPeer loads a block and sends it to the requesting peer,
// if we have it. Otherwise, we'll respond saying we don't have it.
// According to the Tendermint spec, if all nodes are honest,
// no node should be requesting for a block that's non-existent.
func (br *BlockReactor) respondToPeer(msg *_BlockRequestMessage, peer p2p.Peer) (queued bool) {

	block := br.bstore.LoadBlock(msg.Height, msg.Hash)
	if block != nil {
		msgBytes := p2p.TheMK.MustMarshal(p2p.BLOCK_CHANNEL, &_BlockDataMessage{Block: block})
		return peer.TrySend(p2p.BLOCK_CHANNEL, msgBytes)
	}

	br.Logger.Info("Peer asking for a block we don't have", "peer", peer, "height", msg.Height)

	msgBytes := p2p.TheMK.MustMarshal(p2p.BLOCK_CHANNEL, &_BlockMissingMessage{Height: msg.Height, Hash:msg.Hash})
	return peer.TrySend(p2p.BLOCK_CHANNEL, msgBytes)
}

// Receive implements Reactor by handling 4 types of messages (look below).
func (br *BlockReactor) ReceiveMessage(peer p2p.Peer, chId byte, msgBytes []byte) {
	msg, err := p2p.TheMK.Unmarshal(chId, msgBytes)
	if err != nil {
		br.Logger.Error("Error decoding message", "peer", peer, "chId", chId, "msg", msg, "err", err, "bytes", msgBytes)
		br.Adapter.StopPeerForError(peer, err)
		return
	}

	br.Logger.Debug("Receive", "peer", peer, "chId", chId, "msg", msg)

	switch msg := msg.(type) {
	case *_BlockRequestMessage:
		if queued := br.respondToPeer(msg, peer); !queued {
			// Unfortunately not queued since the queue is full.
		}

	case *_BlockDataMessage:
		br.pool.AddBlock(peer.NodeId(), msg.Block, len(msgBytes))

	case *_BlockMissingMessage:
		// nothing to do

	case *_StatusRequestMessage:
		height := br.bstore.Height()
		msgBytes := p2p.TheMK.MustMarshal(p2p.BLOCK_CHANNEL, &_StatusInfoMessage{Height:height, Hash:br.bstore.LoadHash(height)})
		queued := peer.TrySend(p2p.BLOCK_CHANNEL, msgBytes)
		if !queued {
			// sorry
		}

	case *_StatusInfoMessage:
		// Got a peer status. Unverified.
		br.pool.SetPeerHeight(peer.NodeId(), msg.Height)

	default:
		br.Logger.Error(fmt.Sprintf("Unknown message type %v", reflect.TypeOf(msg)))
	}
}

// Handle messages from the BlockPool telling the reactor what to do.
// NOTE: Don't sleep in the FOR_LOOP or otherwise slow it down!
func (br *BlockReactor) poolRoutine() {

	trySyncTicker := time.NewTicker(_TRY_SYNC_INTERVAL)
	statusUpdateTicker := time.NewTicker(_STATUS_UPDATE_INTERVAL)
	switchToConsensusTicker := time.NewTicker(_SWITCH_TO_CONSENSUS_INTERVAL)

	blocksSynced := 0

//	chainId := br.initialState.ChainId
	cst := new(state.ChainState)
	*cst = br.initialState

	lastHundred := time.Now()
	lastRate := 0.0

	c4process := make(chan struct{}, 1)

FOR_LOOP:
	for {
		select {
		case request := <-br.c4request:
			peer := br.Adapter.Peers().Get(request.PeerId)
			if peer == nil {
				continue FOR_LOOP // Peer has since been disconnected.
			}
			msgBytes := p2p.TheMK.MustMarshal(p2p.BLOCK_CHANNEL, &_BlockRequestMessage{Height:request.Height, Hash:request.Hash})
			queued := peer.TrySend(p2p.BLOCK_CHANNEL, msgBytes)
			if !queued {
				// We couldn't make the request, send-queue full.
				// The pool handles timeouts, just let it go.
				continue FOR_LOOP
			}

		case err := <-br.c4error:
			peer := br.Adapter.Peers().Get(err.peerId)
			if peer != nil {
				br.Adapter.StopPeerForError(peer, err)
			}

		case <-statusUpdateTicker.C:
			// ask for status updates
			go br.BroadcastStatusRequest() // nolint: errcheck

		case <-switchToConsensusTicker.C:
			height, numPending, lenRequesters := br.pool.GetStatus()
			outgoing, incoming, _ := br.Adapter.NumPeers()
			br.Logger.Debug("Consensus ticker", "numPending", numPending, "total", lenRequesters,
				"outgoing", outgoing, "incoming", incoming)

			if br.pool.IsCaughtUp() {
				br.Logger.Info("Time to switch to consensus reactor!", "height", height)
				br.pool.Stop()

				cr := br.Adapter.GetReactor("CONSENSUS").(_ConsensusReactor)
				cr.SwitchToConsensus(cst, blocksSynced)

				break FOR_LOOP
			}

		case <-trySyncTicker.C: // chan time
			select {
			case c4process <- struct{}{}:
			default:
			}

		case <-c4process:
			// NOTE: It is a subtle mistake to process more than a single block
			// at a time (e.g. 10) here, because we only TrySend 1 request per
			// loop.  The ratio mismatch can result in starving of blocks, a
			// sudden burst of requests and responses, and repeat.
			// Consequently, it is better to split these routines rather than
			// coupling them as it's written here.  TODO uncouple from request
			// routine.

			// See if there are any blocks to sync.
			first, second := br.pool.PeekTwoBlocks()
			//br.Logger.Info("TrySync peeked", "first", first, "second", second)
			if first == nil || second == nil {
				// We need both to sync the first block.
				continue FOR_LOOP
			} else {
				// Try again quickly next loop.
				c4process <- struct{}{}
			}

			br.pool.PopRequest()

			// TODO: batch saves so we dont persist to disk every block
			br.bstore.SaveBlock(first)

			// TODO: same thing for app - but we would need a way to
			// get the hash without persisting the cst
			var err error
			cst, err = br.bexec.ApplyBlock(cst, first)
			if err != nil {
				// TODO This is bad, are we zombie?
				panic(fmt.Sprintf("Failed to process committed block (%d:%X): %v",
					first.Height, first.Hash(), err))
			}
			blocksSynced++

			if blocksSynced%100 == 0 {
				lastRate = 0.9*lastRate + 0.1*(100/time.Since(lastHundred).Seconds())
				br.Logger.Info("Fast Sync Rate", "height", br.pool.height,
					"max_peer_height", br.pool.MaxPeerHeight(), "blocks/s", lastRate)
				lastHundred = time.Now()
			}
			continue FOR_LOOP

		case <-br.C4Quit():
			break FOR_LOOP
		}
	}
}

// BroadcastStatusRequest broadcasts `BlockStore` height.
func (br *BlockReactor) BroadcastStatusRequest() error {
	height := br.bstore.Height()
	msgBytes := p2p.TheMK.MustMarshal(p2p.BLOCK_CHANNEL, &_StatusRequestMessage{Height:height})
	br.Adapter.Broadcast(p2p.BLOCK_CHANNEL, msgBytes)
	return nil
}

