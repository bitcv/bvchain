package p2p

import (
	"fmt"
	"net"
)

// ErrAdapterDuplicatePeerId to be raised when a peer is connecting with a known
// NodeId.
type ErrAdapterDuplicatePeerId struct {
	Id NodeId
}

func (e ErrAdapterDuplicatePeerId) Error() string {
	return fmt.Sprintf("Duplicate peer Id %v", e.Id)
}

// ErrAdapterDuplicatePeerIp to be raised whena a peer is connecting with a known
// Ip.
type ErrAdapterDuplicatePeerIp struct {
	Ip net.IP
}

func (e ErrAdapterDuplicatePeerIp) Error() string {
	return fmt.Sprintf("Duplicate peer Ip %v", e.Ip.String())
}

// ErrAdapterConnectToSelf to be raised when trying to connect to itself.
type ErrAdapterConnectToSelf struct {
	Addr *NetAddress
}

func (e ErrAdapterConnectToSelf) Error() string {
	return fmt.Sprintf("Connect to self: %v", e.Addr)
}

type ErrAdapterAuthenticationFailure struct {
	Dialed *NetAddress
	Got    NodeId
}

func (e ErrAdapterAuthenticationFailure) Error() string {
	return fmt.Sprintf(
		"Failed to authenticate peer. Dialed %v, but got peer with NodeId %s",
		e.Dialed,
		e.Got,
	)
}

//-------------------------------------------------------------------

type ErrNetAddressNoId struct {
	Addr string
}

func (e ErrNetAddressNoId) Error() string {
	return fmt.Sprintf("Address (%s) does not contain NodeId", e.Addr)
}

type ErrNetAddressInvalid struct {
	Addr string
	Err  error
}

func (e ErrNetAddressInvalid) Error() string {
	return fmt.Sprintf("Invalid address (%s): %v", e.Addr, e.Err)
}

type ErrNetAddressLookup struct {
	Addr string
	Err  error
}

func (e ErrNetAddressLookup) Error() string {
	return fmt.Sprintf("Error looking up host (%s): %v", e.Addr, e.Err)
}
