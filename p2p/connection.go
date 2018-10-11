package p2p

import (
	"bvchain/ec"
	"bvchain/util"
	"bvchain/cfg"
	"bvchain/p2p/cxn"

	"fmt"
	"io"
	"net"
	"sync/atomic"
	"time"
	"halftwo/mangos/vbs"
	"halftwo/mangos/xstr"
	"halftwo/mangos/xerr"
)

type _PeerConnection struct {
	config     *cfg.P2pConfig

	conn       net.Conn	// *cxn.SecretConnection
	ip         net.IP
	outgoing   bool
	persistent bool
}

func (pc *_PeerConnection) NodeId() NodeId {
	return PubKeyToNodeId(pc.conn.(*cxn.SecretConnection).RemotePubKey())
}


var _testIPSuffix uint32

// Return the IP from the connection RemoteAddr
func (pc *_PeerConnection) RemoteIp() net.IP {
	if pc.ip != nil {
		return pc.ip
	}

	// In test cases a conn could not be present at all or be an in-memory
	// implementation where we want to return a fake ip.
	if pc.conn == nil || pc.conn.RemoteAddr().String() == "pipe" {
		pc.ip = net.IP{172, 16, 0, byte(atomic.AddUint32(&_testIPSuffix, 1))}

		return pc.ip
	}

	host, _, err := net.SplitHostPort(pc.conn.RemoteAddr().String())
	util.AssertNoError(err)

	ips, err := net.LookupIP(host)
	util.AssertNoError(err)

	pc.ip = ips[0]

	return pc.ip
}


func newConnectionOutgoing(
	config *cfg.P2pConfig,
	myPrivKey ec.PrivKey,
	addr *NetAddress,
	persistent bool,
) (*_PeerConnection, error) {

	if config.TestDialFail {
                return nil, fmt.Errorf("dial err")
        }

	conn, err := addr.DialTimeout(config.DialTimeout)
	if err != nil {
		return nil, xerr.Trace(err, "Error creating peer")
	}

	pc, err := newConnection(config, myPrivKey, conn, true, persistent)
	if err != nil {
		if cerr := conn.Close(); cerr != nil {
			return nil, xerr.Trace(err, cerr.Error())
		}
		return nil, err
	}

	if addr.NodeId != pc.NodeId() {
		if cerr := conn.Close(); cerr != nil {
			return nil, xerr.Trace(err, cerr.Error())
		}
		return nil, ErrAdapterAuthenticationFailure{addr, pc.NodeId()}
	}

	return pc, nil
}

func newConnectionIncoming(
	config *cfg.P2pConfig,
	myPrivKey ec.PrivKey,
	conn net.Conn,
) (*_PeerConnection, error) {
	return newConnection(config, myPrivKey, conn, false, false)
}

func newConnection(
	config *cfg.P2pConfig,
	myPrivKey ec.PrivKey,
	rawConn net.Conn,
	outgoing, persistent bool,
) (pc *_PeerConnection, err error) {
	conn := rawConn

	if config.TestFuzz {
		conn = FuzzConnAfterFromConfig(conn, 10*time.Second, config.TestFuzzConfig)
	}

	dl := time.Now().Add(config.HandshakeTimeout)
	if err := conn.SetDeadline(dl); err != nil {
		return nil, xerr.Trace(err, "Error setting deadline while encrypting connection")
	}

	conn, err = cxn.MakeSecretConnection(conn, myPrivKey)
	if err != nil {
		return nil, xerr.Trace(err, "Error creating peer")
	}

	return &_PeerConnection{
		config:     config,
		conn:       conn,
		outgoing:   outgoing,
		persistent: persistent,
	}, nil
}

func (pc *_PeerConnection) CloseConn() {
	pc.conn.Close() // nolint: errcheck
}

// Handshake performs the P2P handshake between a given node
// and the peer by exchanging their NodeInfo. It sets the received nodeInfo on
// the peer.
func (pc *_PeerConnection) Handshake(selfInfo *NodeInfo, timeout time.Duration) (peerInfo *NodeInfo, err error) {
	if err := pc.conn.SetDeadline(time.Now().Add(timeout)); err != nil {
		return nil, xerr.Trace(err, "Error setting deadline")
	}

	peerInfo = new(NodeInfo)
	var trs, _ = util.Parallel(
		func(_ int) (val interface{}, err error, abort bool) {
			_, err = marshalNodeInfo(pc.conn, selfInfo)
			return
		},
		func(_ int) (val interface{}, err error, abort bool) {
			_, err = unmarshalNodeInfo(pc.conn, peerInfo)
			return
		},
	)
	if err := trs.FirstError(); err != nil {
		return nil, xerr.Trace(err, "Error during handshake")
	}

	if err := pc.conn.SetDeadline(time.Time{}); err != nil {
		return nil, xerr.Trace(err, "Error removing deadline")
	}

	return peerInfo, nil
}

func marshalNodeInfo(w io.Writer, ni *NodeInfo) (int, error) {
	var n int
	var err error
	if _, ok := w.(*cxn.SecretConnection); ok {
		cw := xstr.NewChunkWriter(w, cxn.SECRET_CHUNK_SIZE)
		enc := vbs.NewEncoder(cw)
		err = enc.Encode(*ni)
		n = enc.Size()
		cw.Flush()
	} else {
		enc := vbs.NewEncoder(w)
		err = enc.Encode(*ni)
		n = enc.Size()
	}

	return n, err
}

func unmarshalNodeInfo(r io.Reader, ni *NodeInfo) (int, error) {
	dec := vbs.NewDecoder(r)
	err := dec.Decode(ni)
	return dec.Size(), err
}

