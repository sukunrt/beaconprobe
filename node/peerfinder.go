package node

import (
	"math/rand/v2"
	"sync"

	"github.com/libp2p/go-libp2p/core/host"
	"github.com/libp2p/go-libp2p/core/network"
	"github.com/libp2p/go-libp2p/core/peer"
)

// PeerFinder stores all discovered peers and provides pull-based access
// for PeerManager instances. It replaces MultiInstanceRouter.
type PeerFinder struct {
	mu      sync.Mutex
	peerSet *SharedPeerSet
	hosts   map[string]host.Host
	peers   map[string]map[peer.ID]peer.AddrInfo // instance name → peers
}

// NewPeerFinder creates a PeerFinder that distributes peers across instances.
func NewPeerFinder(peerSet *SharedPeerSet, hosts map[string]host.Host) *PeerFinder {
	peers := make(map[string]map[peer.ID]peer.AddrInfo, len(hosts))
	for name := range hosts {
		peers[name] = make(map[peer.ID]peer.AddrInfo)
	}
	return &PeerFinder{
		peerSet: peerSet,
		hosts:   hosts,
		peers:   peers,
	}
}

// AddPeer assigns the peer to an instance via SharedPeerSet and stores it.
// Idempotent: updates addrs if called again for the same peer.
func (pf *PeerFinder) AddPeer(ai peer.AddrInfo) {
	owner := pf.peerSet.AssignOrGet(ai.ID)
	pf.mu.Lock()
	defer pf.mu.Unlock()
	if m, ok := pf.peers[owner]; ok {
		m[ai.ID] = ai
	}
}

// Route implements discovery.PeerRouter. It stores the peer for later retrieval.
func (pf *PeerFinder) Route(ai peer.AddrInfo) {
	pf.AddPeer(ai)
}

// GetPeers returns up to count random peers assigned to instance,
// excluding any peer for which skip returns true.
func (pf *PeerFinder) GetPeers(instance string, count int, skip func(peer.ID) bool) []peer.AddrInfo {
	pf.mu.Lock()
	defer pf.mu.Unlock()

	m, ok := pf.peers[instance]
	if !ok {
		return nil
	}

	eligible := make([]peer.ID, 0, len(m))
	for id := range m {
		if skip != nil && skip(id) {
			continue
		}
		eligible = append(eligible, id)
	}

	if len(eligible) == 0 {
		return nil
	}

	rand.Shuffle(len(eligible), func(i, j int) {
		eligible[i], eligible[j] = eligible[j], eligible[i]
	})
	if count > len(eligible) {
		count = len(eligible)
	}

	result := make([]peer.AddrInfo, count)
	for i := range count {
		result[i] = m[eligible[i]]
	}
	return result
}

// IsConnectedToAny returns true if any instance's host is connected to the peer.
func (pf *PeerFinder) IsConnectedToAny(id peer.ID) bool {
	for _, h := range pf.hosts {
		if h.Network().Connectedness(id) == network.Connected {
			return true
		}
	}
	return false
}
