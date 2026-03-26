package node

import (
	"sync"

	"github.com/libp2p/go-libp2p/core/peer"
)

// SharedPeerSet tracks which instance owns each peer.
// Assignments are persistent — once a peer is assigned, it is never unassigned.
// This means the map grows unboundedly over the lifetime of the process; this is
// acceptable for multi-day probe runs (tens of thousands of peers ≈ a few MB).
type SharedPeerSet struct {
	mu          sync.Mutex
	assignments map[peer.ID]string // peer.ID → instance name
	instances   []string
	nextIdx     uint64
}

// NewSharedPeerSet creates a SharedPeerSet for the given instance names.
// Panics if instanceNames is empty.
func NewSharedPeerSet(instanceNames []string) *SharedPeerSet {
	if len(instanceNames) == 0 {
		panic("SharedPeerSet requires at least one instance")
	}
	return &SharedPeerSet{
		assignments: make(map[peer.ID]string),
		instances:   instanceNames,
	}
}

// AssignOrGet returns the owning instance for a peer. If the peer is unassigned,
// it is assigned to the next instance in round-robin order.
func (s *SharedPeerSet) AssignOrGet(id peer.ID) string {
	s.mu.Lock()
	defer s.mu.Unlock()
	if owner, ok := s.assignments[id]; ok {
		return owner
	}
	idx := s.nextIdx
	s.nextIdx++
	owner := s.instances[idx%uint64(len(s.instances))]
	s.assignments[id] = owner
	return owner
}

// Owner returns the owning instance name, or "" if the peer is unassigned.
func (s *SharedPeerSet) Owner(id peer.ID) string {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.assignments[id]
}

// Claim attempts to assign a peer to the given instance. If the peer is already
// assigned, it returns the existing owner. Returns the owner and whether the
// caller successfully claimed it.
func (s *SharedPeerSet) Claim(id peer.ID, instance string) (owner string, claimedByMe bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if existing, ok := s.assignments[id]; ok {
		return existing, existing == instance
	}
	s.assignments[id] = instance
	return instance, true
}
