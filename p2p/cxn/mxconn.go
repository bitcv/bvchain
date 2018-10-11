package cxn

import (
	"bvchain/obj"
	"bvchain/util"
	"bvchain/util/log"
	"bvchain/util/flowrate"

	"bufio"
	"errors"
	"fmt"
	"io"
	"math"
	"net"
	"reflect"
	"sync/atomic"
	"time"
	"halftwo/mangos/xerr"
)

const (
	_DEFAULT_MAX_PACKET_PAYLOAD_SIZE = 1024

	_NUM_BATCH_MSG_PACKET  = 10
	_MIN_READ_BUFFER_SIZE  = 1024
	_MIN_WRITE_BUFFER_SIZE = 65536

	_DEFAULT_SEND_RATE     = 1024*500
	_DEFAULT_RECV_RATE     = 1024*500

	_DEFAULT_SEND_QUEUE_CAPACITY   = 1
	_DEFAULT_RECV_BUFFER_CAPACITY  = 4096
	_DEFAULT_RECV_MESSAGE_CAPACITY = 1024*1024*21

	_SEND_TIMEOUT           = 10 * time.Second
	_UPDATE_INTERVAL        = 2 * time.Second

	_DEFAULT_FLUSH_THROTTLE = 100 * time.Millisecond
	_DEFAULT_PING_INTERVAL  = 60 * time.Second
	_DEFAULT_PONG_TIMEOUT   = 45 * time.Second
)

type _RecvCallback func(chId byte, msgBytes []byte)
type _ErrorCallback func(interface{})

/*
Each peer has one `MxConn` (multiplex connection) instance.

__multiplex__ *noun* a system or signal involving simultaneous transmission of
several messages along a single channel of communication.

Each `MxConn` handles message transmission on multiple abstract communication
`_Channel`s.  Each channel has a globally unique byte id.
The byte id and the relative priorities of each `_Channel` are configured upon
initialization of the connection.

There are two methods for sending messages:
	func (m *MxConn) Send(chId byte, msgBytes []byte) bool {}
	func (m *MxConn) TrySend(chId byte, msgBytes []byte}) bool {}

`Send(chId, msgBytes)` is a blocking call that waits until `msg` is
successfully queued for the channel with the given id byte `chId`, or until the
request times out.  The message `msg` is serialized using Go-Amino.

`TrySend(chId, msgBytes)` is a nonblocking call that returns false if the
channel's queue is full.

Incoming message bytes are handled with an onReceive callback function.
*/
type MxConn struct {
	util.BaseService

	conn          net.Conn
	bufConnReader *bufio.Reader
	bufConnWriter *bufio.Writer
	sendMonitor   *flowrate.Monitor
	recvMonitor   *flowrate.Monitor
	c4Send        chan struct{}
	c4Pong        chan struct{}
	channels      []*_Channel
	channelsIdx   map[byte]*_Channel
	onReceive     _RecvCallback
	onError       _ErrorCallback
	errored       uint32
	config        MxConfig

	c4Quit        chan struct{}
	flushTimer    *time.Timer    // flush writes as necessary but throttled.
	pingTicker    *time.Ticker   // send pings periodically

	// close conn if pong is not received in pongTimeout
	pongTimer     *time.Timer
	c4PongTimeout chan bool // true - timeout, false - peer sent pong

	chStatsTicker *time.Ticker // update channel stats periodically

	created time.Time // time of creation

	maxPacketSize int
}

// MxConfig is a MxConn configuration.
type MxConfig struct {
	SendRate int64 `mapstructure:"send_rate"`
	RecvRate int64 `mapstructure:"recv_rate"`

	// Maximum payload size
	MaxPacketPayloadSize int `mapstructure:"max_packet_msg_payload_size"`

	// Interval to flush writes (throttled)
	FlushThrottle time.Duration `mapstructure:"flush_throttle"`

	// Interval to send pings
	PingInterval time.Duration `mapstructure:"ping_interval"`

	// Maximum wait time for pongs
	PongTimeout time.Duration `mapstructure:"pong_timeout"`
}

// DefaultMxConfig returns the default config.
func DefaultMxConfig() MxConfig {
	return MxConfig{
		SendRate:             _DEFAULT_SEND_RATE,
		RecvRate:             _DEFAULT_RECV_RATE,
		MaxPacketPayloadSize: _DEFAULT_MAX_PACKET_PAYLOAD_SIZE,
		FlushThrottle:        _DEFAULT_FLUSH_THROTTLE,
		PingInterval:         _DEFAULT_PING_INTERVAL,
		PongTimeout:          _DEFAULT_PONG_TIMEOUT,
	}
}

// newMxConn wraps net.Conn and creates multiplex connection
func newMxConn(conn net.Conn, chDescs []*ChannelDescriptor, onReceive _RecvCallback, onError _ErrorCallback) *MxConn {
	return NewMxConnWithConfig(
		conn,
		chDescs,
		onReceive,
		onError,
		DefaultMxConfig())
}

// NewMxConnWithConfig wraps net.Conn and creates multiplex connection with a config
func NewMxConnWithConfig(conn net.Conn, chDescs []*ChannelDescriptor, onReceive _RecvCallback, onError _ErrorCallback, config MxConfig) *MxConn {
	if config.PongTimeout >= config.PingInterval {
		panic("pongTimeout must be less than pingInterval (otherwise, next ping will reset pong timer)")
	}

	mconn := &MxConn{
		conn:          conn,
		bufConnReader: bufio.NewReaderSize(conn, _MIN_READ_BUFFER_SIZE),
		bufConnWriter: bufio.NewWriterSize(conn, _MIN_WRITE_BUFFER_SIZE),
		sendMonitor:   flowrate.New(0, 0),
		recvMonitor:   flowrate.New(0, 0),
		c4Send:        make(chan struct{}, 1),
		c4Pong:        make(chan struct{}, 1),
		onReceive:     onReceive,
		onError:       onError,
		config:        config,
	}
	mconn.BaseService.Init(nil, "MxConn", mconn)

	// Create channels
	var channelsIdx = map[byte]*_Channel{}
	var channels = []*_Channel{}

	for _, desc := range chDescs {
		channel := newChannel(mconn, *desc)
		channelsIdx[channel.desc.Id] = channel
		channels = append(channels, channel)
	}
	mconn.channels = channels
	mconn.channelsIdx = channelsIdx
	mconn.maxPacketSize = mconn.calculateMaxPacketSize()

	return mconn
}

func (c *MxConn) SetLogger(l log.Logger) {
	c.BaseService.SetLogger(l)
	for _, ch := range c.channels {
		ch.SetLogger(l)
	}
}

// OnStart implements BaseService
func (c *MxConn) OnStart() error {
	if err := c.BaseService.OnStart(); err != nil {
		return err
	}
	c.c4Quit = make(chan struct{})
	c.flushTimer = time.NewTimer(c.config.FlushThrottle)
	c.pingTicker = time.NewTicker(c.config.PingInterval)
	c.c4PongTimeout = make(chan bool, 1)
	c.chStatsTicker = time.NewTicker(_UPDATE_INTERVAL)
	go c.sendRoutine()
	go c.recvRoutine()
	return nil
}

// OnStop implements BaseService
func (c *MxConn) OnStop() {
	c.BaseService.OnStop()
	c.flushTimer.Stop()
	c.pingTicker.Stop()
	c.chStatsTicker.Stop()
	if c.c4Quit != nil {
		close(c.c4Quit)
	}
	c.conn.Close() // nolint: errcheck

	// We can't close pong safely here because
	// recvRoutine may write to it after we've stopped.
	// Though it doesn't need to get closed at all,
	// we close it @ recvRoutine.
}

func (c *MxConn) String() string {
	return fmt.Sprintf("MxConn{%v}", c.conn.RemoteAddr())
}

func (c *MxConn) flush() {
	c.Logger.Debug("Flush", "conn", c)
	err := c.bufConnWriter.Flush()
	if err != nil {
		c.Logger.Error("MxConn flush failed", "err", err)
	}
}

// Catch panics, usually caused by remote disconnects.
func (c *MxConn) _recover() {
	if r := recover(); r != nil {
		err := xerr.Trace(r, "recovered panic in MxConn")
		c.Logger.Error("recovered panic", err)
		c.stopForError(err)
	}
}

func (c *MxConn) stopForError(r interface{}) {
	c.Logger.Debug("stopForError", r)
	c.Stop()
	if atomic.CompareAndSwapUint32(&c.errored, 0, 1) {
		if c.onError != nil {
			c.onError(r)
		}
	}
}

// Queues a message to be sent to channel.
func (c *MxConn) Send(chId byte, msgBytes []byte) bool {
	if !c.IsRunning() {
		return false
	}

	c.Logger.Debug("Send", "channel", chId, "conn", c, "msgBytes", fmt.Sprintf("%X", msgBytes))

	// Send message to channel.
	channel, ok := c.channelsIdx[chId]
	if !ok {
		c.Logger.Error(fmt.Sprintf("Cannot send bytes, unknown channel %X", chId))
		return false
	}

	success := channel.sendBytes(msgBytes)
	if success {
		// Wake up sendRoutine if necessary
		select {
		case c.c4Send <- struct{}{}:
		default:
		}
	} else {
		c.Logger.Error("Send failed", "channel", chId, "conn", c, "msgBytes", fmt.Sprintf("%X", msgBytes))
	}
	return success
}

// Queues a message to be sent to channel.
// Nonblocking, returns true if successful.
func (c *MxConn) TrySend(chId byte, msgBytes []byte) bool {
	if !c.IsRunning() {
		return false
	}

	c.Logger.Debug("TrySend", "channel", chId, "conn", c, "msgBytes", fmt.Sprintf("%X", msgBytes))

	// Send message to channel.
	channel, ok := c.channelsIdx[chId]
	if !ok {
		c.Logger.Error(fmt.Sprintf("Cannot send bytes, unknown channel %X", chId))
		return false
	}

	ok = channel.trySendBytes(msgBytes)
	if ok {
		// Wake up sendRoutine if necessary
		select {
		case c.c4Send <- struct{}{}:
		default:
		}
	}

	return ok
}

// CanSend returns true if you can send more data onto the chId, false
// otherwise. Use only as a heuristic.
func (c *MxConn) CanSend(chId byte) bool {
	if !c.IsRunning() {
		return false
	}

	channel, ok := c.channelsIdx[chId]
	if !ok {
		c.Logger.Error(fmt.Sprintf("Unknown channel %X", chId))
		return false
	}
	return channel.canSend()
}

// sendRoutine polls for packets to send from channels.
func (c *MxConn) sendRoutine() {
	defer c._recover()

FOR_LOOP:
	for {
		var n int
		var err error
	SELECTION:
		select {
		case <-c.flushTimer.C:
			c.flush()

		case <-c.chStatsTicker.C:
			for _, channel := range c.channels {
				channel.updateStats()
			}

		case <-c.pingTicker.C:
			c.Logger.Debug("Send Ping")
			n, err = marshalPacket(c.bufConnWriter, _PingPacket{})
			if err != nil {
				break SELECTION
			}
			c.sendMonitor.Update(n)
			c.Logger.Debug("Starting pong timer", "dur", c.config.PongTimeout)
			c.pongTimer = time.AfterFunc(c.config.PongTimeout, func() {
				select {
				case c.c4PongTimeout <- true:
				default:
				}
			})
			c.flush()

		case timeout := <-c.c4PongTimeout:
			if timeout {
				c.Logger.Debug("Pong timeout")
				err = errors.New("pong timeout")
			} else {
				c.stopPongTimer()
			}

		case <-c.c4Pong:
			c.Logger.Debug("Send Pong")
			n, err = marshalPacket(c.bufConnWriter, _PongPacket{})
			if err != nil {
				break SELECTION
			}
			c.sendMonitor.Update(n)
			c.flush()

		case <-c.c4Quit:
			break FOR_LOOP

		case <-c.c4Send:
			// Send some MsgPackets
			eof := c.sendSomeMsgPackets()
			if !eof {
				// Keep sendRoutine awake.
				select {
				case c.c4Send <- struct{}{}:
				default:
				}
			}
		}

		if !c.IsRunning() {
			break FOR_LOOP
		}
		if err != nil {
			c.Logger.Error("Connection failed @ sendRoutine", "conn", c, "err", err)
			c.stopForError(err)
			break FOR_LOOP
		}
	}

	// Cleanup
	c.stopPongTimer()
}

// Returns true if messages from channels were exhausted.
// Blocks in accordance to .sendMonitor throttling.
func (c *MxConn) sendSomeMsgPackets() bool {
	// Block until .sendMonitor says we can write.
	// Once we're ready we send more than we asked for,
	// but amortized it should even out.
	c.sendMonitor.Limit(c.maxPacketSize, atomic.LoadInt64(&c.config.SendRate), true)

	// Now send some MsgPackets.
	for i := 0; i < _NUM_BATCH_MSG_PACKET; i++ {
		if c.sendMsgPacket() {
			return true
		}
	}
	return false
}

// Returns true if messages from channels were exhausted.
func (c *MxConn) sendMsgPacket() bool {
	// Choose a channel to create a MsgPacket from.
	// The chosen channel will be the one whose recentlySent/priority is the least.
	var leastRatio float32 = math.MaxFloat32
	var leastChannel *_Channel
	for _, channel := range c.channels {
		// If nothing to send, skip this channel
		if !channel.isSendPending() {
			continue
		}
		// Get ratio, and keep track of lowest ratio.
		ratio := float32(channel.recentlySent) / float32(channel.desc.Priority)
		if ratio < leastRatio {
			leastRatio = ratio
			leastChannel = channel
		}
	}

	// Nothing to send?
	if leastChannel == nil {
		return true
	}
	// c.Logger.Info("Found a msgPacket to send")

	// Make & send a MsgPacket from this channel
	n, err := leastChannel.writeMsgPacketTo(c.bufConnWriter)
	if err != nil {
		c.Logger.Error("Failed to write _MsgPacket", "err", err)
		c.stopForError(err)
		return true
	}
	c.sendMonitor.Update(n)
	c.flushTimer.Reset(c.config.FlushThrottle)
	return false
}

// recvRoutine reads MsgPackets and reconstructs the message using the channels' "recving" buffer.
// After a whole message has been assembled, it's pushed to onReceive().
// Blocks depending on how the connection is throttled.
// Otherwise, it never blocks.
func (c *MxConn) recvRoutine() {
	defer c._recover()

FOR_LOOP:
	for {
		// Block until .recvMonitor says we can read.
		c.recvMonitor.Limit(c.maxPacketSize, atomic.LoadInt64(&c.config.RecvRate), true)

		var pkt _Packet
		n, err := unmarshalPacket(c.bufConnReader, &pkt, c.config.MaxPacketPayloadSize)
		c.recvMonitor.Update(n)
		if err != nil {
			if c.IsRunning() {
				c.Logger.Error("Connection failed @ recvRoutine (reading byte)", "conn", c, "err", err)
				c.stopForError(err)
			}
			break FOR_LOOP
		}

		// Read more depending on packet type.
		switch pkt := pkt.(type) {
		case _PingPacket:
			// TODO: prevent abuse, as they cause flush()'s.
			c.Logger.Debug("Receive Ping")
			select {
			case c.c4Pong <- struct{}{}:
			default:
				// never block
			}
		case _PongPacket:
			c.Logger.Debug("Receive Pong")
			select {
			case c.c4PongTimeout <- false:
			default:
				// never block
			}
		case _MsgPacket:
			channel, ok := c.channelsIdx[pkt.ChannelId]
			if !ok || channel == nil {
				err := fmt.Errorf("Unknown channel %X", pkt.ChannelId)
				c.Logger.Error("Connection failed @ recvRoutine", "conn", c, "err", err)
				c.stopForError(err)
				break FOR_LOOP
			}

			msgBytes, err := channel.recvMsgPacket(pkt)
			if err != nil {
				if c.IsRunning() {
					c.Logger.Error("Connection failed @ recvRoutine", "conn", c, "err", err)
					c.stopForError(err)
				}
				break FOR_LOOP
			}
			if msgBytes != nil {
				c.Logger.Debug("Received bytes", "chId", pkt.ChannelId, "msgBytes", fmt.Sprintf("%X", msgBytes))
				// NOTE: This means the reactor.Receive runs in the same thread as the p2p recv routine
				c.onReceive(pkt.ChannelId, msgBytes)
			}
		default:
			err := fmt.Errorf("Unknown message type %v", reflect.TypeOf(pkt))
			c.Logger.Error("Connection failed @ recvRoutine", "conn", c, "err", err)
			c.stopForError(err)
			break FOR_LOOP
		}
	}

	// Cleanup
	close(c.c4Pong)
	for range c.c4Pong {
		// Drain
	}
}

// not goroutine-safe
func (c *MxConn) stopPongTimer() {
	if c.pongTimer != nil {
		_ = c.pongTimer.Stop()
		c.pongTimer = nil
	}
}

// calculateMaxPacketSize returns a maximum size of _MsgPacket
func (c *MxConn) calculateMaxPacketSize() int {
	max := c.config.MaxPacketPayloadSize
	// 3 == type(1) + channel(1) + eof(1)
	return 3 + obj.UvarintSize(uint64(max)) + max
}

type ConnStatus struct {
	Duration    time.Duration
	SendMonitor flowrate.Status
	RecvMonitor flowrate.Status
	Channels    []ChannelStatus
}

type ChannelStatus struct {
	Id                byte
	SendQueueCapacity int
	SendQueueSize     int
	Priority          int
	RecentlySent      int64
}

func (c *MxConn) Status() *ConnStatus {
	var status ConnStatus
	status.Duration = time.Since(c.created)
	status.SendMonitor = c.sendMonitor.Status()
	status.RecvMonitor = c.recvMonitor.Status()
	status.Channels = make([]ChannelStatus, len(c.channels))
	for i, channel := range c.channels {
		status.Channels[i] = ChannelStatus{
			Id:                channel.desc.Id,
			SendQueueCapacity: cap(channel.sendQueue),
			SendQueueSize:     channel.loadSendQueueSize(),
			Priority:          channel.desc.Priority,
			RecentlySent:      channel.recentlySent,
		}
	}
	return &status
}

//-----------------------------------------------------------------------------

type ChannelDescriptor struct {
	Id                  byte
	Priority            int
	SendQueueCapacity   int
	RecvBufferCapacity  int
	RecvMessageCapacity int
}

func (chDesc ChannelDescriptor) FillDefaults() (filled ChannelDescriptor) {
	if chDesc.SendQueueCapacity == 0 {
		chDesc.SendQueueCapacity = _DEFAULT_SEND_QUEUE_CAPACITY
	}
	if chDesc.RecvBufferCapacity == 0 {
		chDesc.RecvBufferCapacity = _DEFAULT_RECV_BUFFER_CAPACITY
	}
	if chDesc.RecvMessageCapacity == 0 {
		chDesc.RecvMessageCapacity = _DEFAULT_RECV_MESSAGE_CAPACITY
	}
	filled = chDesc
	return
}

// NOTE: not goroutine-safe.
type _Channel struct {
	conn          *MxConn
	desc          ChannelDescriptor
	sendQueue     chan []byte
	sendQueueSize int32 // atomic.
	recving       []byte
	sending       []byte
	recentlySent  int64 // exponential moving average

	maxPacketPayloadSize int

	Logger log.Logger
}

func newChannel(conn *MxConn, desc ChannelDescriptor) *_Channel {
	desc = desc.FillDefaults()
	if desc.Priority <= 0 {
		panic("_Channel default priority must be a positive integer")
	}
	return &_Channel{
		conn:                 conn,
		desc:                 desc,
		sendQueue:            make(chan []byte, desc.SendQueueCapacity),
		recving:              make([]byte, 0, desc.RecvBufferCapacity),
		maxPacketPayloadSize: conn.config.MaxPacketPayloadSize,
	}
}

func (ch *_Channel) SetLogger(l log.Logger) {
	ch.Logger = l
}

// Queues message to send to this channel.
// Goroutine-safe
// Times out (and returns false) after _SEND_TIMEOUT
func (ch *_Channel) sendBytes(bytes []byte) bool {
	select {
	case ch.sendQueue <- bytes:
		atomic.AddInt32(&ch.sendQueueSize, 1)
		return true
	case <-time.After(_SEND_TIMEOUT):
		return false
	}
}

// Queues message to send to this channel.
// Nonblocking, returns true if successful.
// Goroutine-safe
func (ch *_Channel) trySendBytes(bytes []byte) bool {
	select {
	case ch.sendQueue <- bytes:
		atomic.AddInt32(&ch.sendQueueSize, 1)
		return true
	default:
		return false
	}
}

// Goroutine-safe
func (ch *_Channel) loadSendQueueSize() (size int) {
	return int(atomic.LoadInt32(&ch.sendQueueSize))
}

// Goroutine-safe
// Use only as a heuristic.
func (ch *_Channel) canSend() bool {
	return ch.loadSendQueueSize() < _DEFAULT_SEND_QUEUE_CAPACITY
}

// Returns true if any MsgPackets are pending to be sent.
// Call before calling nextMsgPacket()
// Goroutine-safe
func (ch *_Channel) isSendPending() bool {
	if len(ch.sending) == 0 {
		if len(ch.sendQueue) == 0 {
			return false
		}
		ch.sending = <-ch.sendQueue
	}
	return true
}

// Creates a new _MsgPacket to send.
// Not goroutine-safe
func (ch *_Channel) nextMsgPacket() _MsgPacket {
	pkt := _MsgPacket{}
	pkt.ChannelId = byte(ch.desc.Id)
	maxSize := ch.maxPacketPayloadSize
	pkt.Payload = ch.sending[:util.MinInt(maxSize, len(ch.sending))]
	if len(ch.sending) <= maxSize {
		pkt.Eof = true
		ch.sending = nil
		atomic.AddInt32(&ch.sendQueueSize, -1) // decrement sendQueueSize
	} else {
		pkt.Eof = false
		ch.sending = ch.sending[util.MinInt(maxSize, len(ch.sending)):]
	}
	return pkt
}

// Writes next _MsgPacket to w and updates c.recentlySent.
// Not goroutine-safe
func (ch *_Channel) writeMsgPacketTo(w io.Writer) (n int, err error) {
	pkt := ch.nextMsgPacket()
	n, err = marshalPacket(w, pkt)
	ch.recentlySent += int64(n)
	return
}

// Handles incoming MsgPackets. It returns a message bytes if message is
// complete. NOTE message bytes may change on next call to recvMsgPacket.
// Not goroutine-safe
func (ch *_Channel) recvMsgPacket(pkt _MsgPacket) ([]byte, error) {
	ch.Logger.Debug("Read _MsgPacket", "conn", ch.conn, "packet", pkt)
	recvCap := ch.desc.RecvMessageCapacity
	recvReceived := len(ch.recving) + len(pkt.Payload)
	if recvCap < recvReceived {
		return nil, fmt.Errorf("Received message exceeds available capacity: %v < %v", recvCap, recvReceived)
	}
	ch.recving = append(ch.recving, pkt.Payload...)
	if pkt.Eof {
		msgBytes := ch.recving
		ch.recving = ch.recving[:0]
		return msgBytes, nil
	}
	return nil, nil
}

// Call this periodically to update stats for throttling purposes.
// Not goroutine-safe
func (ch *_Channel) updateStats() {
	// Exponential decay of stats.
	// TODO: optimize.
	ch.recentlySent = int64(float64(ch.recentlySent) * 0.8)
}

//----------------------------------------
type _PacketType uint8

const (
	_PACKET_MSG _PacketType = 'M'
	_PACKET_PING = 'P'
	_PACKET_PONG = 'A'
)

type _Packet interface {
	Type() _PacketType
}

type _PingPacket struct {
}

type _PongPacket struct {
}

type _MsgPacket struct {
	ChannelId byte
	Eof       bool
	Payload   []byte
}

func (_PingPacket) Type() _PacketType { return _PACKET_PING }
func (_PongPacket) Type() _PacketType { return _PACKET_PONG }
func (_MsgPacket) Type() _PacketType { return _PACKET_MSG }

func (mp _MsgPacket) String() string {
	return fmt.Sprintf("_MsgPacket{%X:%X T:%v}", mp.ChannelId, mp.Payload, mp.Eof)
}

func marshalPacket(w io.Writer, pkt _Packet) (n int, err error) {
	s := &obj.Serializer{W:w}
	switch pkt := pkt.(type) {
	case _PingPacket:
		s.WriteByte(byte(_PACKET_PING))
	case _PongPacket:
		s.WriteByte(byte(_PACKET_PONG))
	case _MsgPacket:
		s.WriteByte(byte(_PACKET_MSG))
		s.WriteByte(pkt.ChannelId)
		if pkt.Eof {
			s.WriteByte(1)
		} else {
			s.WriteByte(0)
		}
		s.WriteVariableBytes(pkt.Payload)
	default:
		return 0, fmt.Errorf("marshal unkown _Packet")
	}

	return s.N, s.Err
}

func unmarshalPacket(r io.Reader, pkt *_Packet, max int) (n int, err error) {
	d := &obj.Deserializer{R:r}
	switch pktType := d.ReadByte(); _PacketType(pktType) {
	case _PACKET_PING:
		*pkt = _PingPacket{}
	case _PACKET_PONG:
		*pkt = _PongPacket{}
	case _PACKET_MSG:
		msg := _MsgPacket{}
		msg.ChannelId = d.ReadByte()
		msg.Eof = bool(d.ReadByte()!=0)
		msg.Payload = d.ReadVariableBytes(max, nil)
		*pkt = msg
	default:
		if d.Err == nil {
			return d.N, fmt.Errorf("Read unkown _PacketType(%#x)", pktType)
		}
	}
	return d.N, d.Err
}


