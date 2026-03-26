package node

import (
	"testing"

	"github.com/libp2p/go-libp2p/core/network"
	"github.com/libp2p/go-libp2p/core/peer"
)

func TestSiblingGaterRejectsSiblingDial(t *testing.T) {
	sibling := generatePeerID(t)
	stranger := generatePeerID(t)
	ps := NewSharedPeerSet([]string{"a", "b"})
	g := NewSiblingGater("a", []peer.ID{sibling}, ps)

	if g.InterceptPeerDial(sibling) {
		t.Fatal("expected dial to sibling to be rejected")
	}
	if !g.InterceptPeerDial(stranger) {
		t.Fatal("expected dial to stranger to be allowed")
	}
}

func TestSiblingGaterRejectsSiblingSecured(t *testing.T) {
	sibling := generatePeerID(t)
	ps := NewSharedPeerSet([]string{"a", "b"})
	g := NewSiblingGater("a", []peer.ID{sibling}, ps)

	if g.InterceptSecured(network.DirInbound, sibling, nil) {
		t.Fatal("expected inbound sibling to be rejected")
	}
	if g.InterceptSecured(network.DirOutbound, sibling, nil) {
		t.Fatal("expected outbound sibling to be rejected")
	}
}

func TestSiblingGaterAllowsOutbound(t *testing.T) {
	stranger := generatePeerID(t)
	ps := NewSharedPeerSet([]string{"a", "b"})
	g := NewSiblingGater("a", nil, ps)

	if !g.InterceptSecured(network.DirOutbound, stranger, nil) {
		t.Fatal("expected outbound to stranger to be allowed")
	}
}

func TestSiblingGaterInboundClaimUnassigned(t *testing.T) {
	stranger := generatePeerID(t)
	ps := NewSharedPeerSet([]string{"a", "b"})
	g := NewSiblingGater("a", nil, ps)

	if !g.InterceptSecured(network.DirInbound, stranger, nil) {
		t.Fatal("expected inbound unassigned peer to be accepted (claimed)")
	}

	if owner := ps.Owner(stranger); owner != "a" {
		t.Fatalf("peer should be claimed by a, got %q", owner)
	}
}

func TestSiblingGaterInboundRejectOtherOwner(t *testing.T) {
	stranger := generatePeerID(t)
	ps := NewSharedPeerSet([]string{"a", "b"})

	// Pre-assign to "b".
	ps.Claim(stranger, "b")

	g := NewSiblingGater("a", nil, ps)

	if g.InterceptSecured(network.DirInbound, stranger, nil) {
		t.Fatal("expected inbound peer owned by b to be rejected by a")
	}
}

func TestSiblingGaterInboundAcceptOwnPeer(t *testing.T) {
	stranger := generatePeerID(t)
	ps := NewSharedPeerSet([]string{"a", "b"})

	// Pre-assign to "a".
	ps.Claim(stranger, "a")

	g := NewSiblingGater("a", nil, ps)

	if !g.InterceptSecured(network.DirInbound, stranger, nil) {
		t.Fatal("expected inbound peer owned by a to be accepted by a")
	}
}

func TestSiblingGaterAcceptAndUpgraded(t *testing.T) {
	ps := NewSharedPeerSet([]string{"a"})
	g := NewSiblingGater("a", nil, ps)

	if !g.InterceptAccept(nil) {
		t.Fatal("InterceptAccept should always return true")
	}
	if !g.InterceptAddrDial(generatePeerID(t), nil) {
		t.Fatal("InterceptAddrDial should always return true")
	}
	ok, _ := g.InterceptUpgraded(nil)
	if !ok {
		t.Fatal("InterceptUpgraded should always return true")
	}
}
