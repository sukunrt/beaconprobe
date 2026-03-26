package node

import (
	"github.com/libp2p/go-libp2p/core/control"
	"github.com/libp2p/go-libp2p/core/network"
	"github.com/libp2p/go-libp2p/core/peer"
	ma "github.com/multiformats/go-multiaddr"
)

// SiblingGater prevents sibling probe instances from connecting to each other
// and enforces SharedPeerSet ownership for inbound connections.
type SiblingGater struct {
	siblingIDs map[peer.ID]struct{}
	peerSet    *SharedPeerSet
	myName     string
}

// NewSiblingGater creates a gater that rejects connections to/from sibling
// instance peer IDs and claims inbound peers via the SharedPeerSet.
func NewSiblingGater(myName string, siblingIDs []peer.ID, peerSet *SharedPeerSet) *SiblingGater {
	ids := make(map[peer.ID]struct{}, len(siblingIDs))
	for _, id := range siblingIDs {
		ids[id] = struct{}{}
	}
	return &SiblingGater{
		siblingIDs: ids,
		peerSet:    peerSet,
		myName:     myName,
	}
}

func (g *SiblingGater) InterceptPeerDial(p peer.ID) bool {
	_, isSibling := g.siblingIDs[p]
	return !isSibling
}

func (g *SiblingGater) InterceptAddrDial(_ peer.ID, _ ma.Multiaddr) bool {
	return true
}

func (g *SiblingGater) InterceptAccept(_ network.ConnMultiaddrs) bool {
	return true
}

func (g *SiblingGater) InterceptSecured(dir network.Direction, p peer.ID, _ network.ConnMultiaddrs) bool {
	if _, isSibling := g.siblingIDs[p]; isSibling {
		return false
	}
	// Outbound: allow unconditionally. Callers (PeerRouter/PeerManager) are
	// responsible for only dialing peers assigned to this instance.
	if dir == network.DirOutbound {
		return true
	}
	// Inbound: claim the peer for this instance. If already assigned to
	// another instance, reject the connection.
	owner, _ := g.peerSet.Claim(p, g.myName)
	return owner == g.myName
}

func (g *SiblingGater) InterceptUpgraded(_ network.Conn) (bool, control.DisconnectReason) {
	return true, 0
}
