// Taken from taipei-torrent.
// Just enough UPnP to be able to forward ports
// For more information, see: http://www.upnp-hacks.org/upnp.html
package upnp

// TODO: use syscalls to get actual ourIp, see issue #712

import (
	"bytes"
	"encoding/xml"
	"errors"
	"fmt"
	"io/ioutil"
	"net"
	"net/http"
	"strconv"
	"strings"
	"time"
)

type _UpnpNat struct {
	serviceUrl string
	ourIp      string
	urnDomain  string
}

// protocol is either "udp" or "tcp"
type Nat interface {
	GetExternalAddress() (addr net.IP, err error)
	AddPortMapping(protocol string, externalPort, internalPort int, description string, timeout int) (mappedExternalPort int, err error)
	DeletePortMapping(protocol string, externalPort, internalPort int) (err error)
}

func Discover() (nat Nat, err error) {
	ssdp, err := net.ResolveUDPAddr("udp4", "239.255.255.250:1900")
	if err != nil {
		return
	}
	conn, err := net.ListenPacket("udp4", ":0")
	if err != nil {
		return
	}
	socket := conn.(*net.UDPConn)
	defer socket.Close() // nolint: errcheck

	if err := socket.SetDeadline(time.Now().Add(3 * time.Second)); err != nil {
		return nil, err
	}

	st := "InternetGatewayDevice:1"

	buf := bytes.NewBufferString(
		"M-SEARCH * HTTP/1.1\r\n" +
			"HOST: 239.255.255.250:1900\r\n" +
			"ST: ssdp:all\r\n" +
			"MAN: \"ssdp:discover\"\r\n" +
			"MX: 2\r\n\r\n")
	message := buf.Bytes()
	answerBytes := make([]byte, 1024)
	for i := 0; i < 3; i++ {
		_, err = socket.WriteToUDP(message, ssdp)
		if err != nil {
			return
		}
		var n int
		_, _, err = socket.ReadFromUDP(answerBytes)
		if err != nil {
			return
		}
		for {
			n, _, err = socket.ReadFromUDP(answerBytes)
			if err != nil {
				break
			}
			answer := string(answerBytes[0:n])
			if !strings.Contains(answer, st) {
				continue
			}
			// HTTP header field names are case-insensitive.
			// http://www.w3.org/Protocols/rfc2616/rfc2616-sec4.html#sec4.2
			locString := "\r\nlocation:"
			answer = strings.ToLower(answer)
			locIndex := strings.Index(answer, locString)
			if locIndex < 0 {
				continue
			}
			loc := answer[locIndex+len(locString):]
			endIndex := strings.Index(loc, "\r\n")
			if endIndex < 0 {
				continue
			}
			locUrl := strings.TrimSpace(loc[0:endIndex])
			var serviceUrl, urnDomain string
			serviceUrl, urnDomain, err = getServiceUrl(locUrl)
			if err != nil {
				return
			}
			var ourIp net.IP
			ourIp, err = localIPv4()
			if err != nil {
				return
			}
			nat = &_UpnpNat{serviceUrl: serviceUrl, ourIp: ourIp.String(), urnDomain: urnDomain}
			return
		}
	}
	err = errors.New("UPnP port discovery failed")
	return
}

type _Envelope struct {
	XmlName xml.Name `xml:"http://schemas.xmlsoap.org/soap/envelope/ Envelope"`
	Soap    *_SoapBody
}

type _SoapBody struct {
	XmlName    xml.Name `xml:"http://schemas.xmlsoap.org/soap/envelope/ Body"`
	ExternalIp *_ExternalIpAddressResponse
}

type _ExternalIpAddressResponse struct {
	XmlName   xml.Name `xml:"GetExternalIPAddressResponse"`
	IpAddress string   `xml:"NewExternalIPAddress"`
}

type _ExternalIpAddress struct {
	XmlName xml.Name `xml:"NewExternalIPAddress"`
	Ip      string
}

type _UpnpService struct {
	ServiceType string `xml:"serviceType"`
	ControlUrl  string `xml:"controlUrl"`
}

type _DeviceList struct {
	Device []_Device `xml:"device"`
}

type _ServiceList struct {
	Service []_UpnpService `xml:"service"`
}

type _Device struct {
	XmlName     xml.Name    `xml:"device"`
	DeviceType  string      `xml:"deviceType"`
	DeviceList  _DeviceList  `xml:"deviceList"`
	ServiceList _ServiceList `xml:"serviceList"`
}

type _Root struct {
	Device _Device
}

func getChildDevice(d *_Device, deviceType string) *_Device {
	dl := d.DeviceList.Device
	for i := 0; i < len(dl); i++ {
		if strings.Contains(dl[i].DeviceType, deviceType) {
			return &dl[i]
		}
	}
	return nil
}

func getChildService(d *_Device, serviceType string) *_UpnpService {
	sl := d.ServiceList.Service
	for i := 0; i < len(sl); i++ {
		if strings.Contains(sl[i].ServiceType, serviceType) {
			return &sl[i]
		}
	}
	return nil
}

func localIPv4() (net.IP, error) {
	tt, err := net.Interfaces()
	if err != nil {
		return nil, err
	}
	for _, t := range tt {
		aa, err := t.Addrs()
		if err != nil {
			return nil, err
		}
		for _, a := range aa {
			ipnet, ok := a.(*net.IPNet)
			if !ok {
				continue
			}
			v4 := ipnet.IP.To4()
			if v4 == nil || v4[0] == 127 { // loopback address
				continue
			}
			return v4, nil
		}
	}
	return nil, errors.New("cannot find local IP address")
}

func getServiceUrl(rootUrl string) (url, urnDomain string, err error) {
	r, err := http.Get(rootUrl)
	if err != nil {
		return
	}
	defer r.Body.Close() // nolint: errcheck

	if r.StatusCode >= 400 {
		err = errors.New(string(r.StatusCode))
		return
	}
	var root _Root
	err = xml.NewDecoder(r.Body).Decode(&root)
	if err != nil {
		return
	}
	a := &root.Device
	if !strings.Contains(a.DeviceType, "InternetGatewayDevice:1") {
		err = errors.New("No InternetGatewayDevice")
		return
	}
	b := getChildDevice(a, "WANDevice:1")
	if b == nil {
		err = errors.New("No WANDevice")
		return
	}
	c := getChildDevice(b, "WANConnectionDevice:1")
	if c == nil {
		err = errors.New("No WANConnectionDevice")
		return
	}
	d := getChildService(c, "WANIPConnection:1")
	if d == nil {
		// Some routers don't follow the UPnP spec, and put WanIPConnection under WanDevice,
		// instead of under WanConnectionDevice
		d = getChildService(b, "WANIPConnection:1")

		if d == nil {
			err = errors.New("No WANIPConnection")
			return
		}
	}
	// Extract the domain name, which isn't always 'schemas-upnp-org'
	urnDomain = strings.Split(d.ServiceType, ":")[1]
	url = combineUrl(rootUrl, d.ControlUrl)
	return
}

func combineUrl(rootUrl, subUrl string) string {
	protocolEnd := "://"
	protoEndIndex := strings.Index(rootUrl, protocolEnd)
	a := rootUrl[protoEndIndex+len(protocolEnd):]
	rootIndex := strings.Index(a, "/")
	return rootUrl[0:protoEndIndex+len(protocolEnd)+rootIndex] + subUrl
}

func soapRequest(url, function, message, domain string) (r *http.Response, err error) {
	fullMessage := "<?xml version=\"1.0\" ?>" +
		"<s:Envelope xmlns:s=\"http://schemas.xmlsoap.org/soap/envelope/\" s:encodingStyle=\"http://schemas.xmlsoap.org/soap/encoding/\">\r\n" +
		"<s:Body>" + message + "</s:Body></s:Envelope>"

	req, err := http.NewRequest("POST", url, strings.NewReader(fullMessage))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "text/xml ; charset=\"utf-8\"")
	req.Header.Set("User-Agent", "Darwin/10.0.0, UPnP/1.0, MiniUPnPc/1.3")
	//req.Header.Set("Transfer-Encoding", "chunked")
	req.Header.Set("SOAPAction", "\"urn:"+domain+":service:WANIPConnection:1#"+function+"\"")
	req.Header.Set("Connection", "Close")
	req.Header.Set("Cache-Control", "no-cache")
	req.Header.Set("Pragma", "no-cache")

	// log.Stderr("soapRequest ", req)

	r, err = http.DefaultClient.Do(req)
	if err != nil {
		return nil, err
	}
	/*if r.Body != nil {
	    defer r.Body.Close()
	}*/

	if r.StatusCode >= 400 {
		// log.Stderr(function, r.StatusCode)
		err = errors.New("Error " + strconv.Itoa(r.StatusCode) + " for " + function)
		r = nil
		return
	}
	return
}

type statusInfo struct {
	externalIpAddress string
}

func (n *_UpnpNat) getExternalIPAddress() (info statusInfo, err error) {

	message := "<u:GetExternalIPAddress xmlns:u=\"urn:" + n.urnDomain + ":service:WANIPConnection:1\">\r\n" +
		"</u:GetExternalIPAddress>"

	var response *http.Response
	response, err = soapRequest(n.serviceUrl, "GetExternalIPAddress", message, n.urnDomain)
	if response != nil {
		defer response.Body.Close() // nolint: errcheck
	}
	if err != nil {
		return
	}
	var envelope _Envelope
	data, err := ioutil.ReadAll(response.Body)
	if err != nil {
		return
	}
	reader := bytes.NewReader(data)
	err = xml.NewDecoder(reader).Decode(&envelope)
	if err != nil {
		return
	}

	info = statusInfo{envelope.Soap.ExternalIp.IpAddress}

	if err != nil {
		return
	}

	return
}

// GetExternalAddress returns an external IP. If GetExternalIPAddress action
// fails or IP returned is invalid, GetExternalAddress returns an error.
func (n *_UpnpNat) GetExternalAddress() (addr net.IP, err error) {
	info, err := n.getExternalIPAddress()
	if err != nil {
		return
	}
	addr = net.ParseIP(info.externalIpAddress)
	if addr == nil {
		err = fmt.Errorf("Failed to parse IP: %v", info.externalIpAddress)
	}
	return
}

func (n *_UpnpNat) AddPortMapping(protocol string, externalPort, internalPort int, description string, timeout int) (mappedExternalPort int, err error) {
	// A single concatenation would break ARM compilation.
	message := "<u:AddPortMapping xmlns:u=\"urn:" + n.urnDomain + ":service:WANIPConnection:1\">\r\n" +
		"<NewRemoteHost></NewRemoteHost><NewExternalPort>" + strconv.Itoa(externalPort)
	message += "</NewExternalPort><NewProtocol>" + protocol + "</NewProtocol>"
	message += "<NewInternalPort>" + strconv.Itoa(internalPort) + "</NewInternalPort>" +
		"<NewInternalClient>" + n.ourIp + "</NewInternalClient>" +
		"<NewEnabled>1</NewEnabled><NewPortMappingDescription>"
	message += description +
		"</NewPortMappingDescription><NewLeaseDuration>" + strconv.Itoa(timeout) +
		"</NewLeaseDuration></u:AddPortMapping>"

	var response *http.Response
	response, err = soapRequest(n.serviceUrl, "AddPortMapping", message, n.urnDomain)
	if response != nil {
		defer response.Body.Close() // nolint: errcheck
	}
	if err != nil {
		return
	}

	// TODO: check response to see if the port was forwarded
	// log.Println(message, response)
	// JAE:
	// body, err := ioutil.ReadAll(response.Body)
	// fmt.Println(string(body), err)
	mappedExternalPort = externalPort
	_ = response
	return
}

func (n *_UpnpNat) DeletePortMapping(protocol string, externalPort, internalPort int) (err error) {

	message := "<u:DeletePortMapping xmlns:u=\"urn:" + n.urnDomain + ":service:WANIPConnection:1\">\r\n" +
		"<NewRemoteHost></NewRemoteHost><NewExternalPort>" + strconv.Itoa(externalPort) +
		"</NewExternalPort><NewProtocol>" + protocol + "</NewProtocol>" +
		"</u:DeletePortMapping>"

	var response *http.Response
	response, err = soapRequest(n.serviceUrl, "DeletePortMapping", message, n.urnDomain)
	if response != nil {
		defer response.Body.Close() // nolint: errcheck
	}
	if err != nil {
		return
	}

	// TODO: check response to see if the port was deleted
	// log.Println(message, response)
	_ = response
	return
}
