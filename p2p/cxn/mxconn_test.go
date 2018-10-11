package cxn

import (
	"bvchain/util"
	"bvchain/util/log"

	"bytes"
	"net"
	"testing"
	"time"
	"github.com/fortytw2/leaktest"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

const maxPingPongPacketSize = 1024 // bytes

func createTestMxConn(conn net.Conn) *MxConn {
	onReceive := func(chId byte, msgBytes []byte) {
	}
	onError := func(r interface{}) {
	}
	c := createMxConnWithCallbacks(conn, onReceive, onError)
	c.SetLogger(log.TestingLogger())
	return c
}

func createMxConnWithCallbacks(conn net.Conn, onReceive func(chId byte, msgBytes []byte), onError func(r interface{})) *MxConn {
	config := DefaultMxConfig()
	config.PingInterval = 90 * time.Millisecond
	config.PongTimeout = 45 * time.Millisecond
	chDescs := []*ChannelDescriptor{&ChannelDescriptor{Id: 0x01, Priority: 1, SendQueueCapacity: 1}}
	c := NewMxConnWithConfig(conn, chDescs, onReceive, onError, config)
	c.SetLogger(log.TestingLogger())
	return c
}

func TestMxConnSend(t *testing.T) {
	server, client := net.Pipe()
	defer server.Close() // nolint: errcheck
	defer client.Close() // nolint: errcheck

	mc := createTestMxConn(client)
	err := mc.Start()
	require.Nil(t, err)
	defer mc.Stop()

	msg := []byte("Ant-Man")
	assert.True(t, mc.Send(0x01, msg))
	// Note: subsequent Send/TrySend calls could pass because we are reading from
	// the send queue in a separate goroutine.
	_, err = server.Read(make([]byte, len(msg)))
	if err != nil {
		t.Error(err)
	}
	assert.True(t, mc.CanSend(0x01))

	msg = []byte("Spider-Man")
	assert.True(t, mc.TrySend(0x01, msg))
	_, err = server.Read(make([]byte, len(msg)))
	if err != nil {
		t.Error(err)
	}

	assert.False(t, mc.CanSend(0x05), "CanSend should return false because channel is unknown")
	assert.False(t, mc.Send(0x05, []byte("Absorbing Man")), "Send should return false because channel is unknown")
}

func TestMxConnReceive(t *testing.T) {
	server, client := net.Pipe()
	defer server.Close() // nolint: errcheck
	defer client.Close() // nolint: errcheck

	receivedCh := make(chan []byte)
	errorsCh := make(chan interface{})
	onReceive := func(chId byte, msgBytes []byte) {
		receivedCh <- msgBytes
	}
	onError := func(r interface{}) {
		errorsCh <- r
	}
	mc1 := createMxConnWithCallbacks(client, onReceive, onError)
	err := mc1.Start()
	require.Nil(t, err)
	defer mc1.Stop()

	mc2 := createTestMxConn(server)
	err = mc2.Start()
	require.Nil(t, err)
	defer mc2.Stop()

	msg := []byte("Cyclops")
	assert.True(t, mc2.Send(0x01, msg))

	select {
	case receivedBytes := <-receivedCh:
		assert.Equal(t, []byte(msg), receivedBytes)
	case err := <-errorsCh:
		t.Fatalf("Expected %s, got %+v", msg, err)
	case <-time.After(500 * time.Millisecond):
		t.Fatalf("Did not receive %s message in 500ms", msg)
	}
}

func TestMxConnStatus(t *testing.T) {
	server, client := net.Pipe()
	defer server.Close() // nolint: errcheck
	defer client.Close() // nolint: errcheck

	mc := createTestMxConn(client)
	err := mc.Start()
	require.Nil(t, err)
	defer mc.Stop()

	status := mc.Status()
	assert.NotNil(t, status)
	assert.Zero(t, status.Channels[0].SendQueueSize)
}

func assertPingPacket(t *testing.T, pkt _Packet) {
	_, ok := pkt.(_PingPacket)
	assert.True(t, ok)
}

func assertPongPacket(t *testing.T, pkt _Packet) {
	_, ok := pkt.(_PongPacket)
	assert.True(t, ok)
}

func mustMarshalPacket(pkt _Packet) []byte {
	bf := &bytes.Buffer{}
	_, err := marshalPacket(bf, pkt)
	util.AssertNoError(err)
	return bf.Bytes()
}

func TestMxConnPongTimeoutResultsInError(t *testing.T) {
	server, client := net.Pipe()
	defer server.Close()
	defer client.Close()

	receivedCh := make(chan []byte)
	errorsCh := make(chan interface{})
	onReceive := func(chId byte, msgBytes []byte) {
		receivedCh <- msgBytes
	}
	onError := func(r interface{}) {
		errorsCh <- r
	}
	mc := createMxConnWithCallbacks(client, onReceive, onError)
	err := mc.Start()
	require.Nil(t, err)
	defer mc.Stop()

	serverGotPing := make(chan struct{})
	go func() {
		var pkt _Packet
		_, err = unmarshalPacket(server, &pkt, maxPingPongPacketSize)
		assert.Nil(t, err)
		assertPingPacket(t, pkt)
		serverGotPing <- struct{}{}
	}()
	<-serverGotPing

	pongTimerExpired := mc.config.PongTimeout + 20*time.Millisecond
	select {
	case msgBytes := <-receivedCh:
		t.Fatalf("Expected error, but got %v", msgBytes)
	case err := <-errorsCh:
		assert.NotNil(t, err)
	case <-time.After(pongTimerExpired):
		t.Fatalf("Expected to receive error after %v", pongTimerExpired)
	}
}

func TestMxConnMultiplePongsInTheBeginning(t *testing.T) {
	server, client := net.Pipe()
	defer server.Close()
	defer client.Close()

	receivedCh := make(chan []byte)
	errorsCh := make(chan interface{})
	onReceive := func(chId byte, msgBytes []byte) {
		receivedCh <- msgBytes
	}
	onError := func(r interface{}) {
		errorsCh <- r
	}
	mc := createMxConnWithCallbacks(client, onReceive, onError)
	err := mc.Start()
	require.Nil(t, err)
	defer mc.Stop()

	// sending 3 pongs in a row (abuse)
	_, err = server.Write(mustMarshalPacket(_PongPacket{}))
	require.Nil(t, err)
	_, err = server.Write(mustMarshalPacket(_PongPacket{}))
	require.Nil(t, err)
	_, err = server.Write(mustMarshalPacket(_PongPacket{}))
	require.Nil(t, err)

	serverGotPing := make(chan struct{})
	go func() {
		// read ping (one byte)
		var pkt _Packet
		_, err := unmarshalPacket(server, &pkt, maxPingPongPacketSize)
		require.Nil(t, err)
		assertPingPacket(t, pkt)
		serverGotPing <- struct{}{}
		// respond with pong
		_, err = server.Write(mustMarshalPacket(_PongPacket{}))
		require.Nil(t, err)
	}()
	<-serverGotPing

	pongTimerExpired := mc.config.PongTimeout + 20*time.Millisecond
	select {
	case msgBytes := <-receivedCh:
		t.Fatalf("Expected no data, but got %v", msgBytes)
	case err := <-errorsCh:
		t.Fatalf("Expected no error, but got %v", err)
	case <-time.After(pongTimerExpired):
		assert.True(t, mc.IsRunning())
	}
}

func TestMxConnMultiplePings(t *testing.T) {
	server, client := net.Pipe()
	defer server.Close()
	defer client.Close()

	receivedCh := make(chan []byte)
	errorsCh := make(chan interface{})
	onReceive := func(chId byte, msgBytes []byte) {
		receivedCh <- msgBytes
	}
	onError := func(r interface{}) {
		errorsCh <- r
	}
	mc := createMxConnWithCallbacks(client, onReceive, onError)
	err := mc.Start()
	require.Nil(t, err)
	defer mc.Stop()

	// sending 3 pings in a row (abuse)
	_, err = server.Write(mustMarshalPacket(_PingPacket{}))
	require.Nil(t, err)

	var pkt _Packet
	_, err = unmarshalPacket(server, &pkt, maxPingPongPacketSize)
	require.Nil(t, err)
	assertPongPacket(t, pkt)

	_, err = server.Write(mustMarshalPacket(_PingPacket{}))
	require.Nil(t, err)

	_, err = unmarshalPacket(server, &pkt, maxPingPongPacketSize)
	require.Nil(t, err)
	assertPongPacket(t, pkt)

	_, err = server.Write(mustMarshalPacket(_PingPacket{}))
	require.Nil(t, err)

	_, err = unmarshalPacket(server, &pkt, maxPingPongPacketSize)
	require.Nil(t, err)
	assertPongPacket(t, pkt)

	assert.True(t, mc.IsRunning())
}

func TestMxConnPingPongs(t *testing.T) {
	// check that we are not leaking any go-routines
	defer leaktest.CheckTimeout(t, 10*time.Second)()

	server, client := net.Pipe()

	defer server.Close()
	defer client.Close()

	receivedCh := make(chan []byte)
	errorsCh := make(chan interface{})
	onReceive := func(chId byte, msgBytes []byte) {
		receivedCh <- msgBytes
	}
	onError := func(r interface{}) {
		errorsCh <- r
	}
	mc := createMxConnWithCallbacks(client, onReceive, onError)
	err := mc.Start()
	require.Nil(t, err)
	defer mc.Stop()

	serverGotPing := make(chan struct{})
	go func() {
		// read ping
		var pkt _Packet
		_, err = unmarshalPacket(server, &pkt, maxPingPongPacketSize)
		require.Nil(t, err)
		assertPingPacket(t, pkt)
		serverGotPing <- struct{}{}

		// respond with pong
		_, err = server.Write(mustMarshalPacket(_PongPacket{}))
		require.Nil(t, err)

		time.Sleep(mc.config.PingInterval)

		// read ping
		_, err = unmarshalPacket(server, &pkt, maxPingPongPacketSize)
		require.Nil(t, err)
		assertPingPacket(t, pkt)

		// respond with pong
		_, err = server.Write(mustMarshalPacket(_PongPacket{}))
		require.Nil(t, err)
	}()
	<-serverGotPing

	pongTimerExpired := (mc.config.PongTimeout + 20*time.Millisecond) * 2
	select {
	case msgBytes := <-receivedCh:
		t.Fatalf("Expected no data, but got %v", msgBytes)
	case err := <-errorsCh:
		t.Fatalf("Expected no error, but got %v", err)
	case <-time.After(2 * pongTimerExpired):
		assert.True(t, mc.IsRunning())
	}
}

func TestMxConnStopsAndReturnsError(t *testing.T) {
	server, client := net.Pipe()
	defer server.Close() // nolint: errcheck
	defer client.Close() // nolint: errcheck

	receivedCh := make(chan []byte)
	errorsCh := make(chan interface{})
	onReceive := func(chId byte, msgBytes []byte) {
		receivedCh <- msgBytes
	}
	onError := func(r interface{}) {
		errorsCh <- r
	}
	mc := createMxConnWithCallbacks(client, onReceive, onError)
	err := mc.Start()
	require.Nil(t, err)
	defer mc.Stop()

	if err := client.Close(); err != nil {
		t.Error(err)
	}

	select {
	case receivedBytes := <-receivedCh:
		t.Fatalf("Expected error, got %v", receivedBytes)
	case err := <-errorsCh:
		assert.NotNil(t, err)
		assert.False(t, mc.IsRunning())
	case <-time.After(500 * time.Millisecond):
		t.Fatal("Did not receive error in 500ms")
	}
}

func newClientAndServerConnsForReadErrors(t *testing.T, c4err chan struct{}) (*MxConn, *MxConn) {
	server, client := net.Pipe()

	onReceive := func(chId byte, msgBytes []byte) {}
	onError := func(r interface{}) {}

	// create client conn with two channels
	chDescs := []*ChannelDescriptor{
		{Id: 0x01, Priority: 1, SendQueueCapacity: 1},
		{Id: 0x02, Priority: 1, SendQueueCapacity: 1},
	}
	mcClient := newMxConn(client, chDescs, onReceive, onError)
	mcClient.SetLogger(log.TestingLogger().With("module", "client"))
	err := mcClient.Start()
	require.Nil(t, err)

	// create server conn with 1 channel
	// it fires on c4err when there's an error
	serverLogger := log.TestingLogger().With("module", "server")
	onError = func(r interface{}) {
		c4err <- struct{}{}
	}
	mcServer := createMxConnWithCallbacks(server, onReceive, onError)
	mcServer.SetLogger(serverLogger)
	err = mcServer.Start()
	require.Nil(t, err)
	return mcClient, mcServer
}

func expectSend(ch chan struct{}) bool {
	after := time.After(time.Second * 5)
	select {
	case <-ch:
		return true
	case <-after:
		return false
	}
}

func TestMxConnReadErrorBadEncoding(t *testing.T) {
	c4err := make(chan struct{})
	mcClient, mcServer := newClientAndServerConnsForReadErrors(t, c4err)
	defer mcClient.Stop()
	defer mcServer.Stop()

	client := mcClient.conn

	// send badly encoded msgPacket
	bz := mustMarshalPacket(_MsgPacket{})
	bz[3] += 0x01

	// Write it.
	_, err := client.Write(bz)
	assert.Nil(t, err)
	assert.True(t, expectSend(c4err), "badly encoded msgPacket")
}

func TestMxConnReadErrorUnknownChannel(t *testing.T) {
	c4err := make(chan struct{})
	mcClient, mcServer := newClientAndServerConnsForReadErrors(t, c4err)
	defer mcClient.Stop()
	defer mcServer.Stop()

	msg := []byte("Ant-Man")

	// fail to send msg on channel unknown by client
	assert.False(t, mcClient.Send(0x03, msg))

	// send msg on channel unknown by the server.
	// should cause an error
	assert.True(t, mcClient.Send(0x02, msg))
	assert.True(t, expectSend(c4err), "unknown channel")
}

func TestMxConnReadErrorLongMessage(t *testing.T) {
	c4err := make(chan struct{})
	c4recv := make(chan struct{})

	mcClient, mcServer := newClientAndServerConnsForReadErrors(t, c4err)
	defer mcClient.Stop()
	defer mcServer.Stop()

	mcServer.onReceive = func(chId byte, msgBytes []byte) {
		c4recv <- struct{}{}
	}

	client := mcClient.conn

	// send msg thats just right
	var err error
	pkt := _MsgPacket{
		ChannelId: 0x01,
		Eof:       true,
		Payload:   make([]byte, mcClient.config.MaxPacketPayloadSize),
	}
	buf := mustMarshalPacket(pkt)
	_, err = client.Write(buf)
	assert.Nil(t, err)
	assert.True(t, expectSend(c4recv), "msg length just right")
	assert.False(t, expectSend(c4err), "msg length just right")

	// send msg thats too long
	pkt = _MsgPacket{
		ChannelId: 0x01,
		Eof:       true,
		Payload:   make([]byte, mcClient.config.MaxPacketPayloadSize+100),
	}
	buf = mustMarshalPacket(pkt)
	_, err = client.Write(buf)
	assert.NotNil(t, err)
	assert.False(t, expectSend(c4recv), "msg too long")
	assert.True(t, expectSend(c4err), "msg too long")
}

func TestMxConnReadErrorUnknownMsgType(t *testing.T) {
	c4err := make(chan struct{})
	mcClient, mcServer := newClientAndServerConnsForReadErrors(t, c4err)
	defer mcClient.Stop()
	defer mcServer.Stop()

	// send msg with unknown msg type
	var err error
	_, err = mcClient.conn.Write([]byte{0xFF, 0xFF, 0xFF, 0xFF})
	assert.Nil(t, err)
	assert.True(t, expectSend(c4err), "unknown msg type")
}

func TestMxConnTrySend(t *testing.T) {
	server, client := net.Pipe()
	defer server.Close()
	defer client.Close()

	mc := createTestMxConn(client)
	err := mc.Start()
	require.Nil(t, err)
	defer mc.Stop()

	msg := []byte("Semicolon-Woman")
	resultCh := make(chan string, 2)
	assert.True(t, mc.TrySend(0x01, msg))
	server.Read(make([]byte, len(msg)))
	assert.True(t, mc.CanSend(0x01))
	assert.True(t, mc.TrySend(0x01, msg))
	assert.False(t, mc.CanSend(0x01))
	go func() {
		mc.TrySend(0x01, msg)
		resultCh <- "TrySend"
	}()
	assert.False(t, mc.CanSend(0x01))
	assert.False(t, mc.TrySend(0x01, msg))
	assert.Equal(t, "TrySend", <-resultCh)
}

