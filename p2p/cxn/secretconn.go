package cxn

import (
	"bvchain/ec"
	"bvchain/util"

	"bytes"
	"crypto/aes"
	"encoding/binary"
	"errors"
	"io"
	"net"
	"time"
	"halftwo/mangos/eax"
	"halftwo/mangos/xstr"
	"halftwo/mangos/vbs"
)

const SECRET_CHUNK_SIZE = _DATA_MAX_SIZE

const _DATA_LEN_SIZE = 2
const _DATA_MAX_SIZE = 1024
const _MAC_SIZE = eax.BLOCK_SIZE // 16
const _FRAME_SIZE = _DATA_LEN_SIZE + _DATA_MAX_SIZE + _MAC_SIZE

// Implements net.Conn
type SecretConnection struct {
	conn    net.Conn
	peerPubKey ec.PubKey
	iBuffer []byte
	iNonce  [32]byte
	oNonce  [32]byte
	ix *eax.EaxCtx
	ox *eax.EaxCtx
}

// Performs handshake and returns a new authenticated SecretConnection.
// Returns nil if error in handshake.
// Caller should call conn.Close()
// See docs/sts-final.pdf for more information.
func MakeSecretConnection(conn net.Conn, myPrivKey ec.PrivKey) (*SecretConnection, error) {

	myPubKey := myPrivKey.PubKey()

	myEphPriv := ec.NewPrivKey()
	myEphPub := myEphPriv.PubKey()

	peerEphPub, err := shareEphPubKey(conn, myEphPub)
	if err != nil {
		return nil, err
	}

	isSmall := bytes.Compare(myEphPub[:], peerEphPub[:]) < 0

	iNonce, oNonce := genNonces(myEphPub, peerEphPub, isSmall)

	sharedSecret, err := myEphPriv.SharedSecret(peerEphPub)
	util.AssertNoError(err)

	blockCipher, err := aes.NewCipher(sharedSecret[:32])
	util.AssertNoError(err)

	ix, _ := eax.NewEax(blockCipher)
	ox, _ := eax.NewEax(blockCipher)

	// Construct SecretConnection.
	sc := &SecretConnection{
		conn: conn,
		iBuffer: nil,
		iNonce: iNonce,
		oNonce: oNonce,
		ix: ix,
		ox: ox,
	}

	challenge := genChallenge(myEphPub, peerEphPub, isSmall)
	mySignature := signChallenge(challenge, myPrivKey)

	authMsg, err := shareAuthSignature(sc, myPubKey, mySignature)
	if err != nil {
		return nil, err
	}

	peerPubKey, peerSignature := authMsg.PubKey, authMsg.Signature
	if !peerSignature.VerifyMessagePubKey(challenge[:], peerPubKey) {
		return nil, errors.New("Challenge verification failed")
	}

	// We've authorized.
	sc.peerPubKey = peerPubKey
	return sc, nil
}

// Returns authenticated peerote pubkey
func (sc *SecretConnection) RemotePubKey() ec.PubKey {
	return sc.peerPubKey
}

// Writes encrypted frames of `sealedFrameSize`
// CONTRACT: data smaller than _DATA_MAX_SIZE is read atomically.
func (sc *SecretConnection) Write(data []byte) (n int, err error) {
	for len(data) > 0 {
		frame := make([]byte, _FRAME_SIZE)
		var chunk []byte
		if _DATA_MAX_SIZE < len(data) {
			chunk = data[:_DATA_MAX_SIZE]
			data = data[_DATA_MAX_SIZE:]
		} else {
			chunk = data
			data = nil
		}

		clen := len(chunk)
		binary.BigEndian.PutUint16(frame, uint16(clen))
		copy(frame[_DATA_LEN_SIZE:], chunk)

		sc.encryptFrame(frame)

		_, err := sc.conn.Write(frame)
		if err != nil {
			return n, err
		}
		n += len(chunk)
	}
	return
}

// CONTRACT: data smaller than dataMaxSize is read atomically.
func (sc *SecretConnection) Read(data []byte) (n int, err error) {
	if len(sc.iBuffer) > 0 {
		n = copy(data, sc.iBuffer)
		sc.iBuffer = sc.iBuffer[n:]
		return
	}

	frame := make([]byte, _FRAME_SIZE)
	_, err = io.ReadFull(sc.conn, frame)
	if err != nil {
		return
	}

	ok := sc.decryptFrame(frame)
	if !ok {
		return n, errors.New("Failed to decrypt SecretConnection")
	}

	clen := binary.BigEndian.Uint16(frame) // read the first two bytes
	if clen > _DATA_MAX_SIZE {
		return 0, errors.New("chunkLength is greater than _DATA_MAX_SIZE")
	}
	chunk := frame[_DATA_LEN_SIZE : _DATA_LEN_SIZE + clen]

	n = copy(data, chunk)
	sc.iBuffer = chunk[n:]
	return
}

func (sc *SecretConnection) encryptFrame(frame []byte) {
	util.Assert(len(frame) == _FRAME_SIZE)
	data := frame[:len(frame)-_MAC_SIZE]
	sc.ox.Start(true, sc.oNonce[:], nil)
	sc.ox.Update(data, data)
	sc.ox.Finish(frame[len(frame)-_MAC_SIZE:])
	increase2Nonce(&sc.oNonce)
}

func (sc *SecretConnection) decryptFrame(frame []byte) bool {
	util.Assert(len(frame) == _FRAME_SIZE)
	var mac [_MAC_SIZE]byte
	data := frame[:len(frame)-_MAC_SIZE]
	sc.ix.Start(false, sc.iNonce[:], nil)
	sc.ix.Update(data, data)
	sc.ix.Finish(mac[:])
	increase2Nonce(&sc.iNonce)
	if !bytes.Equal(mac[:], frame[len(frame)-_MAC_SIZE:]) {
		return false
	}
	return true
}

// Implements net.Conn
func (sc *SecretConnection) Close() error                  { return sc.conn.Close() }
func (sc *SecretConnection) LocalAddr() net.Addr           { return sc.conn.LocalAddr() }
func (sc *SecretConnection) RemoteAddr() net.Addr          { return sc.conn.RemoteAddr() }

func (sc *SecretConnection) SetDeadline(t time.Time) error { return sc.conn.SetDeadline(t) }

func (sc *SecretConnection) SetReadDeadline(t time.Time) error {
	return sc.conn.SetReadDeadline(t)
}

func (sc *SecretConnection) SetWriteDeadline(t time.Time) error {
	return sc.conn.SetWriteDeadline(t)
}

func marshalPubKey(w io.Writer, pub ec.PubKey) (int, error) {
	enc := vbs.NewEncoder(w)
	err := enc.Encode(pub)
	return enc.Size(), err
}

func unmarshalPubKey(r io.Reader, pub *ec.PubKey) (int, error) {
	dec := vbs.NewDecoder(r)
	err := dec.Decode(pub)
	return dec.Size(), err
}

func shareEphPubKey(conn net.Conn, myEphPub ec.PubKey) (peerEphPub ec.PubKey, err error) {
	var trs, _ = util.Parallel(
		func(_ int) (val interface{}, err error, abort bool) {
			var _, err1 = marshalPubKey(conn, myEphPub)
			if err1 != nil {
				return nil, err1, true // abort
			} else {
				return nil, nil, false
			}
		},
		func(_ int) (val interface{}, err error, abort bool) {
			var peerEphPub ec.PubKey
			var _, err2 = unmarshalPubKey(conn, &peerEphPub)
			if err2 != nil {
				return nil, err2, true // abort
			} else {
				return peerEphPub, nil, false
			}
		},
	)

	// If error:
	if trs.FirstError() != nil {
		err = trs.FirstError()
		return
	}

	// Otherwise:
	peerEphPub = trs.FirstValue().(ec.PubKey)
	return
}

func genNonces(myPubKey, peerPubKey ec.PubKey, isSmall bool) (ec.Hash256, ec.Hash256) {
	var nonce1 ec.Hash256
	var nonce2 ec.Hash256
	if isSmall {
		nonce1 = ec.Sum256(append(myPubKey[:], peerPubKey[:]...))
	} else {
		nonce1 = ec.Sum256(append(peerPubKey[:], myPubKey[:]...))
	}
	copy(nonce2[:], nonce1[:])
	nonce2[len(nonce2)-1] ^= 0x01

	if (nonce1[len(nonce1)-1] & 0x01) != 0 {
		nonce1, nonce2 = nonce2, nonce1
	}

	if isSmall {
		return nonce1, nonce2
	} else {
		return nonce2, nonce1
	}
}

func genChallenge(myPubKey, peerPubKey ec.PubKey, isSmall bool) []byte {
	var h ec.Hash512
	if isSmall {
		h = ec.Sum512(append(myPubKey[:], peerPubKey[:]...))
	} else {
		h = ec.Sum512(append(peerPubKey[:], myPubKey[:]...))
	}
	return h[:]
}

func signChallenge(challenge []byte, myPrivKey ec.PrivKey) (signature ec.Signature) {
	signature, err := myPrivKey.SignMessage(challenge[:])
	util.AssertNoError(err)
	return
}

type _AuthMessage struct {
	PubKey ec.PubKey	`json:"pubkey"`
	Signature ec.Signature	`json:"sig"`
}

func marshalAuthMessage(w io.Writer, msg *_AuthMessage) (int, error) {
	cw := xstr.NewChunkWriter(w, SECRET_CHUNK_SIZE)
	enc := vbs.NewEncoder(cw)
	err := enc.Encode(*msg)
	cw.Flush()
	return enc.Size(), err
}

func unmarshalAuthMessage(r io.Reader, msg *_AuthMessage) (int, error) {
	dec := vbs.NewDecoder(r)
	err := dec.Decode(msg)
	return dec.Size(), err
}

func shareAuthSignature(sc *SecretConnection, pubKey ec.PubKey, signature ec.Signature) (msg _AuthMessage, err error) {
	var trs, _ = util.Parallel(
		func(_ int) (val interface{}, err error, abort bool) {
			var _, err1 = marshalAuthMessage(sc, &_AuthMessage{pubKey, signature})
			if err1 != nil {
				return nil, err1, true // abort
			} else {
				return nil, nil, false
			}
		},
		func(_ int) (val interface{}, err error, abort bool) {
			var msg _AuthMessage
			var _, err2 = unmarshalAuthMessage(sc, &msg)
			if err2 != nil {
				return nil, err2, true // abort
			} else {
				return msg, nil, false
			}
		},
	)

	// If error:
	if trs.FirstError() != nil {
		err = trs.FirstError()
		return
	}

	msg = trs.FirstValue().(_AuthMessage)
	return msg, nil
}

// increase nonce by 2
func increase2Nonce(nonce *[32]byte) {
	increaseNonce(nonce)
	increaseNonce(nonce)
}

// increase nonce by 1
func increaseNonce(nonce *[32]byte) {
	for i := 31; i >= 0; i-- {
		nonce[i]++
		if nonce[i] != 0 {
			return
		}
	}
}

