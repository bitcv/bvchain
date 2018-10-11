package tx

import (
	"bvchain/util/log"
	"bvchain/cfg"
	"bvchain/p2p"

	"fmt"
	"reflect"
	"time"
	"container/list"
)

const _MAX_MSG_SIZE = 1024*1024
const _PEER_CATCHUP_SLEEP_INTERVAL = 100 * time.Millisecond

// TrxPoolReactor handles TrxPool trx broadcasting amongst peers.
type TrxPoolReactor struct {
	p2p.BaseReactor
	config  *cfg.TrxPoolConfig
	TrxPool *TrxPool
}

// NewTrxPoolReactor returns a new TrxPoolReactor with the given config and TrxPool.
func NewTrxPoolReactor(config *cfg.TrxPoolConfig, tp *TrxPool) *TrxPoolReactor {
	tr := &TrxPoolReactor{
		config:  config,
		TrxPool: tp,
	}
	tr.BaseReactor.Init("TrxPoolReactor", tr)
	return tr
}

// SetLogger sets the Logger on the reactor and the underlying TrxPool.
func (tr *TrxPoolReactor) SetLogger(l log.Logger) {
	tr.Logger = l
	tr.TrxPool.SetLogger(l)
}

// OnStart implements p2p.BaseReactor.
func (tr *TrxPoolReactor) OnStart() error {
	if !tr.config.Broadcast {
		tr.Logger.Info("Trx broadcasting is disabled")
	}
	return nil
}

// GetChannels implements Reactor.
// It returns the list of channels for this reactor.
func (tr *TrxPoolReactor) GetChannels() []*p2p.ChannelDescriptor {
	return []*p2p.ChannelDescriptor{
		{
			Id: p2p.TRX_CHANNEL,
			Priority: 5,
		},
	}
}

// AddPeer implements Reactor.
// It starts a broadcast routine ensuring all trxs are forwarded to the given peer.
func (tr *TrxPoolReactor) AddPeer(peer p2p.Peer) {
	go tr.broadcastTrxRoutine(peer)
}

// RemovePeer implements Reactor.
func (tr *TrxPoolReactor) RemovePeer(peer p2p.Peer, reason interface{}) {
	// what to do?
}

// Receive implements Reactor.
// It adds any received transactions to the TrxPool.
func (tr *TrxPoolReactor) ReceiveMessage(src p2p.Peer, chId byte, msgBytes []byte) {
	msg, err := p2p.TheMK.Unmarshal(chId, msgBytes)
	if err != nil {
		tr.Logger.Error("Error decoding message", "src", src, "chId", chId, "msg", msg, "err", err, "bytes", msgBytes)
		tr.Adapter.StopPeerForError(src, err)
		return
	}
	tr.Logger.Debug("Receive", "src", src, "chId", chId, "msg", msg)

	switch msg := msg.(type) {
	case *_TrxMessage:
		err := tr.TrxPool.CheckAndPush(msg.Trx)
		if err != nil {
			tr.Logger.Info("Could not check trx", "trx", TrxId(msg.Trx), "err", err)
		}
		// broadcasting happens from go routines per peer
	default:
		tr.Logger.Error(fmt.Sprintf("Unknown message type %v", reflect.TypeOf(msg)))
	}
}

// PeerState describes the state of a peer.
type PeerState interface {
	GetHeight() int64
}

// Send new TrxPool trxs to peer.
func (tr *TrxPoolReactor) broadcastTrxRoutine(peer p2p.Peer) {
	if !tr.config.Broadcast {
		return
	}

	var next *list.Element
	for {
		// This happens because the CElement we were looking at got garbage
		// collected (removed). That is, .NextWait() returned nil. Go ahead and
		// start from the beginning.
		if next == nil {
			select {
		/* TODO:
			case <-tr.TrxPool.C4Trxs(): // Wait until a trx is available
				if next = tr.TrxPool.TrxsFront(); next == nil {
					continue
				}
		*/
			case <-peer.C4Quit():
				return
			case <-tr.C4Quit():
				return
			}
		}

		mTrx := next.Value.(*_MyTrx)
		// make sure the peer is up to date
		height := mTrx.Height()
		if peerState_i := peer.Get(p2p.PeerStateKey); peerState_i != nil {
			peerState := peerState_i.(PeerState)
			peerHeight := peerState.GetHeight()
			if peerHeight < height-1 { // Allow for a lag of 1 block
				time.Sleep(_PEER_CATCHUP_SLEEP_INTERVAL)
				continue
			}
		}
		// send mTrx
		msg := &_TrxMessage{Trx: mTrx.trx}
		success := peer.Send(p2p.TRX_CHANNEL, p2p.TheMK.MustMarshal(p2p.TRX_CHANNEL, msg))
		if !success {
			time.Sleep(_PEER_CATCHUP_SLEEP_INTERVAL)
			continue
		}

		select {
		/* TODO
		case <-next.NextWaitChan():
			// see the start of the for loop for nil check
			next = next.Next()
		*/
		case <-peer.C4Quit():
			return

		case <-tr.C4Quit():
			return
		}
	}
}

