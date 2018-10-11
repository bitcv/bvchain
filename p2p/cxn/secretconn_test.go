package cxn

import (
	"bvchain/util"
	"bvchain/ec"

	"testing"
	"fmt"
	"io"
	"net"
	"time"
	"math/rand"

	"halftwo/mangos/crock32"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

type _PipeConn struct {
	*io.PipeReader
	*io.PipeWriter
}

// Implements net.Conn
func (c _PipeConn) Close() (err error) {
	err2 := c.PipeWriter.CloseWithError(io.EOF)
	err1 := c.PipeReader.Close()
	if err2 != nil {
		return err2
	}
	return err1
}

func (c _PipeConn) LocalAddr() net.Addr           { return &net.UnixAddr{"local-test-name", "local-test-net"} }
func (c _PipeConn) RemoteAddr() net.Addr          { return &net.UnixAddr{"remote-test-name", "remote-test-net"} }
func (c _PipeConn) SetDeadline(t time.Time) error { return nil }
func (c _PipeConn) SetReadDeadline(t time.Time) error { return nil }
func (c _PipeConn) SetWriteDeadline(t time.Time) error { return nil }


func randStr(n int) string {
	buf := make([]byte, n)
	for i := 0; i < n; i++ {
		buf[i] = crock32.AlphabetLower[rand.Intn(32)]
	}
	return string(buf)
}

// Each returned ReadWriteCloser is akin to a net.Conn
func makePipeConnPair() (fooConn, barConn _PipeConn) {
	barReader, fooWriter := io.Pipe()
	fooReader, barWriter := io.Pipe()
	return _PipeConn{fooReader, fooWriter}, _PipeConn{barReader, barWriter}
}

func makeSecretConnPair(tb testing.TB) (fooSecConn, barSecConn *SecretConnection) {

	var fooConn, barConn = makePipeConnPair()
	var fooPrvKey = ec.NewPrivKey()
	var fooPubKey = fooPrvKey.PubKey()
	var barPrvKey = ec.NewPrivKey()
	var barPubKey = barPrvKey.PubKey()

	// Make connections from both sides in parallel.
	var trs, ok = util.Parallel(
		func(_ int) (val interface{}, err error, abort bool) {
			fooSecConn, err = MakeSecretConnection(fooConn, fooPrvKey)
			if err != nil {
				tb.Errorf("Failed to establish SecretConnection for foo: %v", err)
				return nil, err, true
			}
			remotePubBytes := fooSecConn.RemotePubKey()
			if !remotePubBytes.Equal(barPubKey) {
				err = fmt.Errorf("Unexpected fooSecConn.RemotePubKey.  Expected %v, got %v",
					barPubKey, fooSecConn.RemotePubKey())
				tb.Error(err)
				return nil, err, false
			}
			return nil, nil, false
		},
		func(_ int) (val interface{}, err error, abort bool) {
			barSecConn, err = MakeSecretConnection(barConn, barPrvKey)
			if barSecConn == nil {
				tb.Errorf("Failed to establish SecretConnection for bar: %v", err)
				return nil, err, true
			}
			remotePubBytes := barSecConn.RemotePubKey()
			if !remotePubBytes.Equal(fooPubKey) {
				err = fmt.Errorf("Unexpected barSecConn.RemotePubKey.  Expected %v, got %v",
					fooPubKey, barSecConn.RemotePubKey())
				tb.Error(err)
				return nil, nil, false
			}
			return nil, nil, false
		},
	)

	require.Nil(tb, trs.FirstError())
	require.True(tb, ok, "Unexpected task abortion")

	return
}

func TestSecretConnectionHandshake(t *testing.T) {
	fooSecConn, barSecConn := makeSecretConnPair(t)
	if err := fooSecConn.Close(); err != nil {
		t.Error(err)
	}
	if err := barSecConn.Close(); err != nil {
		t.Error(err)
	}
}

func TestSecretConnectionReadWrite(t *testing.T) {
	fooConn, barConn := makePipeConnPair()
	fooWrites, barWrites := []string{}, []string{}
	fooReads, barReads := []string{}, []string{}

	// Pre-generate the things to write (for foo & bar)
	for i := 0; i < 100; i++ {
		fooWrites = append(fooWrites, randStr((rand.Int()%(_DATA_MAX_SIZE*5))+1))
		barWrites = append(barWrites, randStr((rand.Int()%(_DATA_MAX_SIZE*5))+1))
	}

	// A helper that will run with (fooConn, fooWrites, fooReads) and vice versa
	genNodeRunner := func(id string, nodeConn _PipeConn, nodeWrites []string, nodeReads *[]string) util.Task {
		return func(_ int) (interface{}, error, bool) {
			// Initiate cryptographic private key and secret connection trhough nodeConn.
			nodePrvKey := ec.NewPrivKey()
			nodeSecretConn, err := MakeSecretConnection(nodeConn, nodePrvKey)
			if err != nil {
				t.Errorf("Failed to establish SecretConnection for node: %v", err)
				return nil, err, true
			}
			// In parallel, handle some reads and writes.
			var trs, ok = util.Parallel(
				func(_ int) (interface{}, error, bool) {
					// Node writes:
					for _, nodeWrite := range nodeWrites {
						n, err := nodeSecretConn.Write([]byte(nodeWrite))
						if err != nil {
							t.Errorf("Failed to write to nodeSecretConn: %v", err)
							return nil, err, true
						}
						if n != len(nodeWrite) {
							err = fmt.Errorf("Failed to write all bytes. Expected %v, wrote %v", len(nodeWrite), n)
							t.Error(err)
							return nil, err, true
						}
					}
					if err := nodeConn.PipeWriter.Close(); err != nil {
						t.Error(err)
						return nil, err, true
					}
					return nil, nil, false
				},
				func(_ int) (interface{}, error, bool) {
					// Node reads:
					readBuffer := make([]byte, _DATA_MAX_SIZE)
					for {
						n, err := nodeSecretConn.Read(readBuffer)
						if err == io.EOF {
							return nil, nil, false
						} else if err != nil {
							t.Errorf("Failed to read from nodeSecretConn: %v", err)
							return nil, err, true
						}
						*nodeReads = append(*nodeReads, string(readBuffer[:n]))
					}
					if err := nodeConn.PipeReader.Close(); err != nil {
						t.Error(err)
						return nil, err, true
					}
					return nil, nil, false
				},
			)
			assert.True(t, ok, "Unexpected task abortion")

			// If error:
			if trs.FirstError() != nil {
				return nil, trs.FirstError(), true
			}

			// Otherwise:
			return nil, nil, false
		}
	}

	// Run foo & bar in parallel
	var trs, ok = util.Parallel(
		genNodeRunner("foo", fooConn, fooWrites, &fooReads),
		genNodeRunner("bar", barConn, barWrites, &barReads),
	)
	require.Nil(t, trs.FirstError())
	require.True(t, ok, "unexpected task abortion")

	// A helper to ensure that the writes and reads match.
	// Additionally, small writes (<= _DATA_MAX_SIZE) must be atomically read.
	compareWritesReads := func(writes []string, reads []string) {
		for {
			// Pop next write & corresponding reads
			var read, write string = "", writes[0]
			var readCount = 0
			for _, readChunk := range reads {
				read += readChunk
				readCount++
				if len(write) <= len(read) {
					break
				}
				if len(write) <= _DATA_MAX_SIZE {
					break // atomicity of small writes
				}
			}
			// Compare
			if write != read {
				t.Errorf("Expected to read %X, got %X", write, read)
			}
			// Iterate
			writes = writes[1:]
			reads = reads[readCount:]
			if len(writes) == 0 {
				break
			}
		}
	}

	compareWritesReads(fooWrites, barReads)
	compareWritesReads(barWrites, fooReads)

}

func BenchmarkSecretConnection(b *testing.B) {
	b.StopTimer()
	fooSecConn, barSecConn := makeSecretConnPair(b)
	fooWriteText := randStr(_DATA_MAX_SIZE)
	// Consume reads from bar's reader
	go func() {
		readBuffer := make([]byte, _DATA_MAX_SIZE)
		for {
			_, err := barSecConn.Read(readBuffer)
			if err == io.EOF {
				return
			} else if err != nil {
				b.Fatalf("Failed to read from barSecConn: %v", err)
			}
		}
	}()

	b.StartTimer()
	for i := 0; i < b.N; i++ {
		_, err := fooSecConn.Write([]byte(fooWriteText))
		if err != nil {
			b.Fatalf("Failed to write to fooSecConn: %v", err)
		}
	}
	b.StopTimer()

	if err := fooSecConn.Close(); err != nil {
		b.Error(err)
	}
}

