package node

import (
	"log/slog"

	"github.com/libp2p/go-libp2p/core/host"
	"github.com/libp2p/go-libp2p/core/network"
	"github.com/libp2p/go-libp2p/core/peer"
)

// MultiInstanceRouter routes discovered peers to the appropriate instance's
// PeerManager based on SharedPeerSet assignment. It implements discovery.PeerRouter.
type MultiInstanceRouter struct {
	peerSet    *SharedPeerSet
	candidates map[string]chan<- peer.AddrInfo
	hosts      map[string]host.Host
}

// NewMultiInstanceRouter creates a router that distributes peers across instances.
func NewMultiInstanceRouter(
	peerSet *SharedPeerSet,
	candidates map[string]chan<- peer.AddrInfo,
	hosts map[string]host.Host,
) *MultiInstanceRouter {
	return &MultiInstanceRouter{
		peerSet:    peerSet,
		candidates: candidates,
		hosts:      hosts,
	}
}

// Route assigns the peer to an instance via SharedPeerSet and sends it to
// the corresponding PeerManager's candidates channel.
func (r *MultiInstanceRouter) Route(ai peer.AddrInfo) {
	owner := r.peerSet.AssignOrGet(ai.ID)
	ch, ok := r.candidates[owner]
	if !ok {
		slog.Error("router: no candidates channel for instance", "instance", owner)
		return
	}
	select {
	case ch <- ai:
	default:
		// Channel full — drop the candidate to avoid blocking discovery.
	}
}

// IsConnectedToAny returns true if any instance's host is connected to the peer.
func (r *MultiInstanceRouter) IsConnectedToAny(id peer.ID) bool {
	for _, h := range r.hosts {
		if h.Network().Connectedness(id) == network.Connected {
			return true
		}
	}
	return false
}
