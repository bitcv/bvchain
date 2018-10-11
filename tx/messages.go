package tx

import (
	"bvchain/p2p"

	"fmt"
	"reflect"
)


func init() {
	man := p2p.TheMK.RegisterChannel(p2p.TRX_CHANNEL)
	man.Register(p2p.MessageCode(0x01), reflect.TypeOf((*_TrxMessage)(nil)))
}

type _TrxMessage struct {
	Trx Trx
}

func (m *_TrxMessage) String() string {
	return fmt.Sprintf("[Trx %v]", m.Trx)
}

