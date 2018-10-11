package p2p

import (
	"net"
	"sync"
)

// ImmutableePeerSet has a (immutable) subset of the methods of PeerSet.
type ImmutablePeerSet interface {
	Get(key NodeId) Peer
	Has(key NodeId) bool
	HasIp(ip net.IP) bool
	List() []Peer
	Size() int
}

//-----------------------------------------------------------------------------

// PeerSet is a special structure for keeping a table of peers.
// Iteration over the peers is super fast and thread-safe.
type PeerSet struct {
	mtx    sync.Mutex
	lookup map[NodeId]*_SetItem
	list   []Peer
}

type _SetItem struct {
	peer  Peer
	index int
}

// NewPeerSet creates a new peerSet with a list of initial capacity of 256 items.
func NewPeerSet() *PeerSet {
	return &PeerSet{
		lookup: make(map[NodeId]*_SetItem),
		list:   make([]Peer, 0, 256),
	}
}

// Add adds the peer to the PeerSet.
// It returns an error carrying the reason, if the peer is already present.
func (ps *PeerSet) Add(peer Peer) error {
	ps.mtx.Lock()
	defer ps.mtx.Unlock()

	if ps.lookup[peer.NodeId()] != nil {
		return ErrAdapterDuplicatePeerId{peer.NodeId()}
	}

	index := len(ps.list)
	// Appending is safe even with other goroutines
	// iterating over the ps.list slice.
	ps.list = append(ps.list, peer)
	ps.lookup[peer.NodeId()] = &_SetItem{peer, index}
	return nil
}

// Has returns true if the set contains the peer referred to by this
// id, otherwise false.
func (ps *PeerSet) Has(id NodeId) bool {
	ps.mtx.Lock()
	_, ok := ps.lookup[id]
	ps.mtx.Unlock()
	return ok
}

// HasIP returns true if the set contains the peer referred to by this IP
// address, otherwise false.
func (ps *PeerSet) HasIp(ip net.IP) bool {
	ps.mtx.Lock()
	defer ps.mtx.Unlock()

	return ps.hasIp(ip)
}

// hasIP does not acquire a lock so it can be used in public methods which
// already lock.
func (ps *PeerSet) hasIp(ip net.IP) bool {
	for _, item := range ps.lookup {
		if item.peer.RemoteIp().Equal(ip) {
			return true
		}
	}

	return false
}

// Get looks up a peer by the provided id. Returns nil if peer is not
// found.
func (ps *PeerSet) Get(id NodeId) Peer {
	ps.mtx.Lock()
	defer ps.mtx.Unlock()
	item, ok := ps.lookup[id]
	if ok {
		return item.peer
	}
	return nil
}

// Remove discards peer by its Key, if the peer was previously memoized.
func (ps *PeerSet) Remove(peer Peer) {
	ps.mtx.Lock()
	defer ps.mtx.Unlock()

	item := ps.lookup[peer.NodeId()]
	if item == nil {
		return
	}

	delete(ps.lookup, peer.NodeId())
	index := item.index
	if index == len(ps.list) - 1 {
		ps.list = ps.list[:len(ps.list)-1]
		return
	}

	newList := make([]Peer, len(ps.list)-1)
	copy(newList, ps.list)

	// Replace the popped item with the last item in the old list.
	lastPeer := ps.list[len(ps.list)-1]
	lastPeerKey := lastPeer.NodeId()
	lastPeerItem := ps.lookup[lastPeerKey]
	newList[index] = lastPeer
	lastPeerItem.index = index
	ps.list = newList
}

// Size returns the number of unique items in the peerSet.
func (ps *PeerSet) Size() int {
	ps.mtx.Lock()
	defer ps.mtx.Unlock()
	return len(ps.list)
}

// List returns the threadsafe list of peers.
func (ps *PeerSet) List() []Peer {
	ps.mtx.Lock()
	defer ps.mtx.Unlock()
	return ps.list
}

