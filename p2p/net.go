package p2p

import (
	"net"
	"strings"
)

// Connect dials the given address and returns a net.Conn. The protoAddr argument should be prefixed with the protocol,
// eg. "tcp://127.0.0.1:8080" or "unix:///tmp/test.sock"
func Connect(protoAddr string) (net.Conn, error) {
	proto, address := SplitProtocolAndAddress(protoAddr)
	conn, err := net.Dial(proto, address)
	return conn, err
}

// ProtocolAndAddress splits an address into the protocol and address components.
// For instance, "tcp://127.0.0.1:8080" will be split into "tcp" and "127.0.0.1:8080".
// If the address has no protocol prefix, the default is "tcp".
func SplitProtocolAndAddress(listenAddr string) (protocol string, addr string) {
	parts := strings.SplitN(listenAddr, "://", 2)
	if len(parts) == 2 {
		protocol, addr = parts[0], parts[1]
	} else {
		protocol, addr = "tcp", listenAddr
	}
	return
}

