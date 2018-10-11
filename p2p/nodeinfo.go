package p2p

import (
	"fmt"
	"strings"
)

type NodeInfo struct {
        NodeId     NodeId
        ListenAddr string

        ChainId    string
        Version    string
	Channels   []byte
}

const (
	_MAX_NUM_CHANNELS = 32
)


func idAddressString(id NodeId, hostPort string) string {
        return fmt.Sprintf("%s@%s", id, hostPort)
}


func (ni *NodeInfo) NetAddress() *NetAddress {
	netAddr, err := NewNetAddressString(idAddressString(ni.NodeId, ni.ListenAddr))
	if err != nil {
		switch err.(type) {
		case ErrNetAddressLookup:
			// TODO
		default:
			panic(err) // can't reach here? 
		}
	}
	return netAddr
}

func (ni *NodeInfo) Validate() error {
	if len(ni.Channels) > _MAX_NUM_CHANNELS {
		return fmt.Errorf("NodeInfo.Channels is too long (%v). Max is %v", len(ni.Channels), _MAX_NUM_CHANNELS)
	}

	channels := make(map[byte]struct{})
	for _, ch := range ni.Channels {
		_, ok := channels[ch]
		if ok {
			return fmt.Errorf("NodeInfo.Channels contains duplicate channel id %v", ch)
		}
		channels[ch] = struct{}{}
	}

	// ensure ListenAddr is good
	_, err := NewNetAddressString(idAddressString(ni.NodeId, ni.ListenAddr))
	return err
}

func (ni *NodeInfo) CompatibleWith(other *NodeInfo) error {
	iMajor, iMinor, _, iErr := splitVersion(ni.Version)
	oMajor, oMinor, _, oErr := splitVersion(other.Version)

	// if our own version number is not formatted right, we messed up
	if iErr != nil {
		return iErr
	}

	// version number must be formatted correctly ("x.x.x")
	if oErr != nil {
		return oErr
	}

	// major version must match
	if iMajor != oMajor {
		return fmt.Errorf("Peer is on a different major version. Got %v, expected %v", oMajor, iMajor)
	}

	// minor version can differ
	if iMinor != oMinor {
		// ok
	}

	// nodes must be on the same ChainId
	if ni.ChainId != other.ChainId {
		return fmt.Errorf("Peer is on a different network. Got %v, expected %v", other.ChainId, ni.ChainId)
	}

	// if we have no channels, we're just testing
	if len(ni.Channels) == 0 {
		return nil
	}

	// for each of our channels, check if they have it
	found := false
OUTER_LOOP:
	for _, ch1 := range ni.Channels {
		for _, ch2 := range other.Channels {
			if ch1 == ch2 {
				found = true
				break OUTER_LOOP // only need one
			}
		}
	}
	if !found {
		return fmt.Errorf("Peer has no common channels. Our channels: %v ; Peer channels: %v", ni.Channels, other.Channels)
	}
	return nil
}

func splitVersion(version string) (string, string, string, error) {
	vs := strings.Split(version, ".")
	if len(vs) != 3 {
		return "", "", "", fmt.Errorf("Invalid version format %v", version)
	}
	return vs[0], vs[1], vs[2], nil
}

