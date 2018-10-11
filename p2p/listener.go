package p2p

import (
	"bvchain/util"
	"bvchain/util/log"
	"bvchain/p2p/upnp"

	"fmt"
	"net"
	"strconv"
	"strings"
	"time"
)

// Listener is a network listener for stream-oriented protocols, providing
// convenient methods to get listener's internal and external addresses.
// Clients are supposed to read incoming connections from a channel, returned
// by Connections() method.
type Listener interface {
	InternalAddress() *NetAddress
	ExternalAddress() *NetAddress
	ExternalAddressHost() string
	String() string
	Stop() error
	C4Conn() <-chan net.Conn
}

// _Listener is a util.Service, running net.Listener underneath.
// Optionally, UPnP is used upon calling NewListener to resolve external
// address.
type _Listener struct {
	util.BaseService

	listener net.Listener
	iAddr  *NetAddress
	eAddr  *NetAddress
	c4conn chan net.Conn
}

const (
	_BUFFERED_CXN_NUMBER	 = 10
	_DEFAULT_EXTERNAL_PORT	 = 9876
	_TRY_LISTEN_TIMES	 = 5
)

func splitHostPort(addr string) (host string, port int) {
	host, portStr, err := net.SplitHostPort(addr)
	util.AssertNoError(err)
	port, err = strconv.Atoi(portStr)
	util.AssertNoError(err)
	return host, port
}

// NewListener creates a new _Listener on lAddr, optionally trying
// to determine external address using UPnP.
func NewListener(
	fullListenAddr string,
	externalAddr string,
	useUPnP bool,
	logger log.Logger) Listener {

	// Split protocol, address, and port.
	protocol, lAddr := SplitProtocolAndAddress(fullListenAddr)
	lAddrIP, lAddrPort := splitHostPort(lAddr)

	// Create listener
	var listener net.Listener
	var err error
	for i := 0; i < _TRY_LISTEN_TIMES; i++ {
		listener, err = net.Listen(protocol, lAddr)
		if err == nil {
			break
		} else if i < _TRY_LISTEN_TIMES-1 {
			time.Sleep(time.Second * 1)
		}
	}
	util.AssertNoError(err)
	// Actual listener local IP & port
	listenerIP, listenerPort := splitHostPort(listener.Addr().String())
	logger.Info("Local listener", "ip", listenerIP, "port", listenerPort)

	// Determine internal address...
	var intAddr *NetAddress
	intAddr, err = NewNetAddressStringWithOptionalId(lAddr)
	util.AssertNoError(err)

	inAddrAny := lAddrIP == "" || lAddrIP == "0.0.0.0"

	// Determine external address.
	var extAddr *NetAddress

	if externalAddr != "" {
		var err error
		extAddr, err = NewNetAddressStringWithOptionalId(externalAddr)
		util.AssertNoError(err)
	}

	// If the lAddrIP is INADDR_ANY, try UPnP.
	if extAddr == nil && useUPnP && inAddrAny {
		extAddr = getUpnpExternalAddress(lAddrPort, listenerPort, logger)
	}

	// Otherwise just use the local address.
	if extAddr == nil {
		defaultToIPv4 := inAddrAny
		extAddr = getNaiveExternalAddress(defaultToIPv4, listenerPort, false, logger)
	}
	if extAddr == nil {
		panic("Could not determine external address!")
	}

	l := &_Listener{
		listener: listener,
		iAddr:    intAddr,
		eAddr:    extAddr,
		c4conn: make(chan net.Conn, _BUFFERED_CXN_NUMBER),
	}
	l.BaseService.Init(logger, "_Listener", l)

	err = l.Start()
	if err != nil {
		logger.Error("Error starting base service", "err", err)
	}
	return l
}

// OnStart implements util.Service by spinning a goroutine, listening for new
// connections.
func (l *_Listener) OnStart() error {
	if err := l.BaseService.OnStart(); err != nil {
		return err
	}
	go l.listenRoutine()
	return nil
}

// OnStop implements util.Service by closing the listener.
func (l *_Listener) OnStop() {
	l.BaseService.OnStop()
	l.listener.Close() // nolint: errcheck
}

// Accept connections and pass on the channel
func (l *_Listener) listenRoutine() {
	for {
		conn, err := l.listener.Accept()

		if !l.IsRunning() {
			break
		}

		util.AssertNoError(err)

		l.c4conn <- conn
	}

	close(l.c4conn)
	for range l.c4conn {
		// Drain
	}
}

// Connections returns a channel of incoming connections.
// It gets closed when the listener closes.
func (l *_Listener) C4Conn() <-chan net.Conn {
	return l.c4conn
}

// InternalAddress returns the internal NetAddress (address used for
// listening).
func (l *_Listener) InternalAddress() *NetAddress {
	return l.iAddr
}

// ExternalAddress returns the external NetAddress (publicly available,
// determined using either UPnP or local resolver).
func (l *_Listener) ExternalAddress() *NetAddress {
	return l.eAddr
}

// ExternalAddressHost returns the external NetAddress IP string. If an IP is
// IPv6, it's wrapped in brackets ("[2001:db8:1f70::999:de8:7648:6e8]").
func (l *_Listener) ExternalAddressHost() string {
	ip := l.ExternalAddress().Ip
	if isIpv6(ip) {
		return "[" + ip.String() + "]"
	}
	return ip.String()
}

func (l *_Listener) String() string {
	return fmt.Sprintf("Listener(@%v)", l.eAddr)
}

/* external address helpers */

// UPNP external address discovery & port mapping
func getUpnpExternalAddress(externalPort, internalPort int, logger log.Logger) *NetAddress {
	logger.Info("Getting UPNP external address")
	nat, err := upnp.Discover()
	if err != nil {
		logger.Info("Could not perform UPNP discover", "err", err)
		return nil
	}

	ext, err := nat.GetExternalAddress()
	if err != nil {
		logger.Info("Could not get UPNP external address", "err", err)
		return nil
	}

	// UPnP can't seem to get the external port, so let's just be explicit.
	if externalPort == 0 {
		externalPort = _DEFAULT_EXTERNAL_PORT
	}

	externalPort, err = nat.AddPortMapping("tcp", externalPort, internalPort, "bvchain", 0)
	if err != nil {
		logger.Info("Could not add UPNP port mapping", "err", err)
		return nil
	}

	logger.Info("Got UPNP external address", "address", ext)
	return NewNetAddressIpPort(ext, uint16(externalPort))
}

func isIpv6(ip net.IP) bool {
	v4 := ip.To4()
	if v4 != nil {
		return false
	}

	ipString := ip.String()

	// Extra check just to be sure it's IPv6
	return (strings.Contains(ipString, ":") && !strings.Contains(ipString, "."))
}

// TODO: use syscalls: see issue #712
func getNaiveExternalAddress(defaultToIPv4 bool, port int, settleForLocal bool, logger log.Logger) *NetAddress {
	addrs, err := net.InterfaceAddrs()
	util.AssertNoError(err)

	for _, a := range addrs {
		ipnet, ok := a.(*net.IPNet)
		if !ok {
			continue
		}
		if defaultToIPv4 || !isIpv6(ipnet.IP) {
			v4 := ipnet.IP.To4()
			if v4 == nil || (!settleForLocal && v4[0] == 127) {
				// loopback
				continue
			}
		} else if !settleForLocal && ipnet.IP.IsLoopback() {
			// IPv6, check for loopback
			continue
		}
		return NewNetAddressIpPort(ipnet.IP, uint16(port))
	}

	// try again, but settle for local
	logger.Info("Node may not be connected to internet. Settling for local address")
	return getNaiveExternalAddress(defaultToIPv4, port, true, logger)
}
