package p2p

import (
	"bvchain/cfg"

	"net"
	"sync"
	"time"
	"math/rand"
)

// FuzzedConnection wraps any net.Conn and depending on the mode either delays
// reads/writes or randomly drops reads/writes/connections.
type FuzzedConnection struct {
	conn net.Conn

	mtx sync.Mutex
	c4start <-chan time.Time
	active bool

	config *cfg.FuzzConnConfig
}

// FuzzConn creates a new FuzzedConnection. Fuzzing starts immediately.
func FuzzConn(conn net.Conn) net.Conn {
	return FuzzConnFromConfig(conn, cfg.DefaultFuzzConnConfig())
}

// FuzzConnFromConfig creates a new FuzzedConnection from a config. Fuzzing
// starts immediately.
func FuzzConnFromConfig(conn net.Conn, config *cfg.FuzzConnConfig) net.Conn {
	return &FuzzedConnection{
		conn: conn,
		c4start: make(<-chan time.Time),
		active: true,
		config: config,
	}
}

// FuzzConnAfter creates a new FuzzedConnection. Fuzzing starts when the
// duration elapses.
func FuzzConnAfter(conn net.Conn, d time.Duration) net.Conn {
	return FuzzConnAfterFromConfig(conn, d, cfg.DefaultFuzzConnConfig())
}

// FuzzConnAfterFromConfig creates a new FuzzedConnection from a config.
// Fuzzing starts when the duration elapses.
func FuzzConnAfterFromConfig(
	conn net.Conn,
	d time.Duration,
	config *cfg.FuzzConnConfig,
) net.Conn {
	return &FuzzedConnection{
		conn: conn,
		c4start: time.After(d),
		active: false,
		config: config,
	}
}

// Config returns the connection's config.
func (fc *FuzzedConnection) Config() *cfg.FuzzConnConfig {
	return fc.config
}

// Read implements net.Conn.
func (fc *FuzzedConnection) Read(data []byte) (n int, err error) {
	if fc.fuzz() {
		return 0, nil
	}
	return fc.conn.Read(data)
}

// Write implements net.Conn.
func (fc *FuzzedConnection) Write(data []byte) (n int, err error) {
	if fc.fuzz() {
		return 0, nil
	}
	return fc.conn.Write(data)
}

// Close implements net.Conn.
func (fc *FuzzedConnection) Close() error { return fc.conn.Close() }

// LocalAddr implements net.Conn.
func (fc *FuzzedConnection) LocalAddr() net.Addr { return fc.conn.LocalAddr() }

// RemoteAddr implements net.Conn.
func (fc *FuzzedConnection) RemoteAddr() net.Addr { return fc.conn.RemoteAddr() }

// SetDeadline implements net.Conn.
func (fc *FuzzedConnection) SetDeadline(t time.Time) error { return fc.conn.SetDeadline(t) }

// SetReadDeadline implements net.Conn.
func (fc *FuzzedConnection) SetReadDeadline(t time.Time) error {
	return fc.conn.SetReadDeadline(t)
}

// SetWriteDeadline implements net.Conn.
func (fc *FuzzedConnection) SetWriteDeadline(t time.Time) error {
	return fc.conn.SetWriteDeadline(t)
}

func (fc *FuzzedConnection) randomDuration() time.Duration {
	maxDelayMillis := int(fc.config.MaxDelay.Nanoseconds() / 1000)
	return time.Millisecond * time.Duration(rand.Int()%maxDelayMillis) // nolint: gas
}

// implements the fuzz (delay, kill conn)
// and returns whether or not the read/write should be ignored
func (fc *FuzzedConnection) fuzz() bool {
	if !fc.shouldFuzz() {
		return false
	}

	switch fc.config.Mode {
	case cfg.FuzzModeDrop:
		// randomly drop the r/w, drop the conn, or sleep
		r := rand.Float64()
		if r <= fc.config.ProbDropRW {
			return true
		} else if r < fc.config.ProbDropRW+fc.config.ProbDropConn {
			// XXX: can't this fail because machine precision?
			// XXX: do we need an error?
			fc.Close() // nolint: errcheck, gas
			return true
		} else if r < fc.config.ProbDropRW+fc.config.ProbDropConn+fc.config.ProbSleep {
			time.Sleep(fc.randomDuration())
		}
	case cfg.FuzzModeDelay:
		// sleep a bit
		time.Sleep(fc.randomDuration())
	}
	return false
}

func (fc *FuzzedConnection) shouldFuzz() bool {
	if fc.active {
		return true
	}

	fc.mtx.Lock()
	defer fc.mtx.Unlock()

	select {
	case <-fc.c4start:
		fc.active = true
		return true
	default:
		return false
	}
}
