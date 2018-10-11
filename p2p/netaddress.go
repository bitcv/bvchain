package p2p

import (
	"net"
	"flag"
	"fmt"
	"time"
	"strings"
	"strconv"
)

const NodeIdByteLength = 32
const NodeIdLength = 52

type NetAddress struct {
	NodeId NodeId
	Ip     net.IP
	Port   uint16

	_str string
}

func JoinIpPort(ip net.IP, port uint16) string {
	return net.JoinHostPort(ip.String(), strconv.FormatUint(uint64(port), 10))
}

func (na *NetAddress) MarshalJSON() ([]byte, error) {
	return []byte(strconv.Quote(na.String())), nil
}

func (na *NetAddress) UnmarshalJSON(bz []byte) error {
	if len(bz) < 2 || bz[0] != '"' || bz[len(bz)-1] != '"' {
		return fmt.Errorf("Invalid NetAddress string");
	}

	addr, err := NewNetAddressString(string(bz[1:len(bz)-1]))
	if err != nil {
		return err
	}

	*na = *addr
	return nil
}

func NewNetAddress(id NodeId, addr net.Addr) *NetAddress {
	tcpAddr, ok := addr.(*net.TCPAddr)
	if !ok {
		if flag.Lookup("test.v") == nil { // normal run
			panic(fmt.Sprintf("Only TCPAddrs are supported. Got: %v", addr))
		} else { // in testing
			netAddr := NewNetAddressIpPort(net.IP("0.0.0.0"), 0)
			netAddr.NodeId = id
			return netAddr
		}
	}
	ip := tcpAddr.IP
	port := uint16(tcpAddr.Port)
	na := NewNetAddressIpPort(ip, port)
	na.NodeId = id
	return na
}

// NewNetAddressString returns a new NetAddress using the provided address in
// the form of "Id@Ip:Port".
// Also resolves the host if host is not an IP.
// Errors are of type ErrNetAddressXxx where Xxx is in (NoId, Invalid, Lookup)
func NewNetAddressString(addr string) (*NetAddress, error) {
	spl := strings.Split(addr, "@")
	if len(spl) < 2 {
		return nil, ErrNetAddressNoId{addr}
	}
	return NewNetAddressStringWithOptionalId(addr)
}

// NewNetAddressStringWithOptionalId returns a new NetAddress using the
// provided address in the form of "Id@Ip:Port", where the Id is optional.
// Also resolves the host if host is not an IP.
func NewNetAddressStringWithOptionalId(addr string) (*NetAddress, error) {
	addrWithoutProtocol := removeProtocolIfDefined(addr)

	var id NodeId
	spl := strings.Split(addrWithoutProtocol, "@")
	if len(spl) == 2 {
		idStr := spl[0]
		if !IsValidNodeId(idStr) {
			return nil, ErrNetAddressInvalid{addrWithoutProtocol, fmt.Errorf("Invalid NodeId string")}
		}
		id, addrWithoutProtocol = NodeId(idStr), spl[1]
	}

	host, portStr, err := net.SplitHostPort(addrWithoutProtocol)
	if err != nil {
		return nil, ErrNetAddressInvalid{addrWithoutProtocol, err}
	}

	ip := net.ParseIP(host)
	if ip == nil {
		if len(host) > 0 {
			ips, err := net.LookupIP(host)
			if err != nil {
				return nil, ErrNetAddressLookup{host, err}
			}
			ip = ips[0]
		}
	}

	port, err := strconv.ParseUint(portStr, 10, 16)
	if err != nil {
		return nil, ErrNetAddressInvalid{portStr, err}
	}

	na := NewNetAddressIpPort(ip, uint16(port))
	na.NodeId = id
	return na, nil
}

// NewNetAddressStrings returns an array of NetAddress'es build using
// the provided strings.
func NewNetAddressStrings(addrs []string) ([]*NetAddress, []error) {
	netAddrs := make([]*NetAddress, 0)
	errs := make([]error, 0)
	for _, addr := range addrs {
		netAddr, err := NewNetAddressString(addr)
		if err != nil {
			errs = append(errs, err)
		} else {
			netAddrs = append(netAddrs, netAddr)
		}
	}
	return netAddrs, errs
}

// NewNetAddressIpPort returns a new NetAddress using the provided IP
// and port number.
func NewNetAddressIpPort(ip net.IP, port uint16) *NetAddress {
	return &NetAddress{
		Ip:   ip,
		Port: port,
	}
}

// Equal reports whether na and other are the same addresses,
// including their Id, Ip, and Port.
func (na *NetAddress) Equal(other *NetAddress) bool {
	return na.String() == other.String()
}

// IsSame returns true is na has the same non-empty Id or DialString as other.
func (na *NetAddress) IsSame(other *NetAddress) bool {
	if na.DialString() == other.DialString() {
		return true
	}
	if na.NodeId != "" && na.NodeId == other.NodeId {
		return true
	}
	return false
}

// String representation: <Id>@<Ip>:<Port>
func (na *NetAddress) String() string {
	if na._str == "" {
		addrStr := na.DialString()
		if na.NodeId != "" {
			addrStr = fmt.Sprintf("%s@%s", na.NodeId, addrStr)
		}
		na._str = addrStr
	}
	return na._str
}

func (na *NetAddress) DialString() string {
	return net.JoinHostPort(
		na.Ip.String(),
		strconv.FormatUint(uint64(na.Port), 10),
	)
}

// Dial calls net.Dial on the address.
func (na *NetAddress) Dial() (net.Conn, error) {
	conn, err := net.Dial("tcp", na.DialString())
	if err != nil {
		return nil, err
	}
	return conn, nil
}

// DialTimeout calls net.DialTimeout on the address.
func (na *NetAddress) DialTimeout(timeout time.Duration) (net.Conn, error) {
	conn, err := net.DialTimeout("tcp", na.DialString(), timeout)
	if err != nil {
		return nil, err
	}
	return conn, nil
}

// Routable returns true if the address is routable.
func (na *NetAddress) Routable() bool {
	// TODO(oga) bitcoind doesn't include RFC3849 here, but should we?
	return na.Valid() && !(na.RFC1918() || na.RFC3927() || na.RFC4862() ||
		na.RFC4193() || na.RFC4843() || na.Local())
}

// For IPv4 these are either a 0 or all bits set address. For IPv6 a zero
// address or one that matches the RFC3849 documentation address format.
func (na *NetAddress) Valid() bool {
	return na.Ip != nil && !(na.Ip.IsUnspecified() || na.RFC3849() ||
		na.Ip.Equal(net.IPv4bcast))
}

// Local returns true if it is a local address.
func (na *NetAddress) Local() bool {
	return na.Ip.IsLoopback() || zero4.Contains(na.Ip)
}

// ReachabilityTo checks whenever o can be reached from na.
func (na *NetAddress) ReachabilityTo(o *NetAddress) int {
	const (
		Unreachable = 0
		Default     = iota
		Teredo
		Ipv6_weak
		Ipv4
		Ipv6_strong
	)
	if !na.Routable() {
		return Unreachable
	} else if na.RFC4380() {
		if !o.Routable() {
			return Default
		} else if o.RFC4380() {
			return Teredo
		} else if o.Ip.To4() != nil {
			return Ipv4
		} else { // ipv6
			return Ipv6_weak
		}
	} else if na.Ip.To4() != nil {
		if o.Routable() && o.Ip.To4() != nil {
			return Ipv4
		}
		return Default
	} else /* ipv6 */ {
		var tunnelled bool
		// Is our v6 is tunnelled?
		if o.RFC3964() || o.RFC6052() || o.RFC6145() {
			tunnelled = true
		}
		if !o.Routable() {
			return Default
		} else if o.RFC4380() {
			return Teredo
		} else if o.Ip.To4() != nil {
			return Ipv4
		} else if tunnelled {
			// only prioritise ipv6 if we aren't tunnelling it.
			return Ipv6_weak
		}
		return Ipv6_strong
	}
}

// RFC1918: IPv4 Private networks (10.0.0.0/8, 192.168.0.0/16, 172.16.0.0/12)
// RFC3849: IPv6 Documentation address  (2001:0DB8::/32)
// RFC3927: IPv4 Autoconfig (169.254.0.0/16)
// RFC3964: IPv6 6to4 (2002::/16)
// RFC4193: IPv6 unique local (FC00::/7)
// RFC4380: IPv6 Teredo tunneling (2001::/32)
// RFC4843: IPv6 ORCHID: (2001:10::/28)
// RFC4862: IPv6 Autoconfig (FE80::/64)
// RFC6052: IPv6 well known prefix (64:FF9B::/96)
// RFC6145: IPv6 IPv4 translated address ::FFFF:0:0:0/96
var rfc1918_10 = net.IPNet{IP: net.ParseIP("10.0.0.0"), Mask: net.CIDRMask(8, 32)}
var rfc1918_192 = net.IPNet{IP: net.ParseIP("192.168.0.0"), Mask: net.CIDRMask(16, 32)}
var rfc1918_172 = net.IPNet{IP: net.ParseIP("172.16.0.0"), Mask: net.CIDRMask(12, 32)}
var rfc3849 = net.IPNet{IP: net.ParseIP("2001:0DB8::"), Mask: net.CIDRMask(32, 128)}
var rfc3927 = net.IPNet{IP: net.ParseIP("169.254.0.0"), Mask: net.CIDRMask(16, 32)}
var rfc3964 = net.IPNet{IP: net.ParseIP("2002::"), Mask: net.CIDRMask(16, 128)}
var rfc4193 = net.IPNet{IP: net.ParseIP("FC00::"), Mask: net.CIDRMask(7, 128)}
var rfc4380 = net.IPNet{IP: net.ParseIP("2001::"), Mask: net.CIDRMask(32, 128)}
var rfc4843 = net.IPNet{IP: net.ParseIP("2001:10::"), Mask: net.CIDRMask(28, 128)}
var rfc4862 = net.IPNet{IP: net.ParseIP("FE80::"), Mask: net.CIDRMask(64, 128)}
var rfc6052 = net.IPNet{IP: net.ParseIP("64:FF9B::"), Mask: net.CIDRMask(96, 128)}
var rfc6145 = net.IPNet{IP: net.ParseIP("::FFFF:0:0:0"), Mask: net.CIDRMask(96, 128)}
var zero4 = net.IPNet{IP: net.ParseIP("0.0.0.0"), Mask: net.CIDRMask(8, 32)}

func (na *NetAddress) RFC1918() bool {
	return rfc1918_10.Contains(na.Ip) ||
		rfc1918_192.Contains(na.Ip) ||
		rfc1918_172.Contains(na.Ip)
}

func (na *NetAddress) RFC3849() bool { return rfc3849.Contains(na.Ip) }
func (na *NetAddress) RFC3927() bool { return rfc3927.Contains(na.Ip) }
func (na *NetAddress) RFC3964() bool { return rfc3964.Contains(na.Ip) }
func (na *NetAddress) RFC4193() bool { return rfc4193.Contains(na.Ip) }
func (na *NetAddress) RFC4380() bool { return rfc4380.Contains(na.Ip) }
func (na *NetAddress) RFC4843() bool { return rfc4843.Contains(na.Ip) }
func (na *NetAddress) RFC4862() bool { return rfc4862.Contains(na.Ip) }
func (na *NetAddress) RFC6052() bool { return rfc6052.Contains(na.Ip) }
func (na *NetAddress) RFC6145() bool { return rfc6145.Contains(na.Ip) }

func removeProtocolIfDefined(addr string) string {
	if strings.Contains(addr, "://") {
		return strings.Split(addr, "://")[1]
	}
	return addr

}

