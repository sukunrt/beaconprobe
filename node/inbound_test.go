package node

import (
	"testing"
	"time"

	"github.com/libp2p/go-libp2p/core/network"
	"github.com/libp2p/go-libp2p/core/peer"
)

func TestHandleInboundPeers_ClaimUnassigned(t *testing.T) {
	ps := NewSharedPeerSet([]string{"a", "b"})
	h := newTestHost(t)
	remote := newTestHost(t)

	ctx := t.Context()
	go HandleInboundPeers(ctx, h, "a", ps)

	// Connect remote → h (inbound to h).
	if err := remote.Connect(ctx, peer.AddrInfo{ID: h.ID(), Addrs: h.Addrs()}); err != nil {
		t.Fatal(err)
	}

	time.Sleep(200 * time.Millisecond)

	// Peer should be claimed by "a".
	owner := ps.Owner(remote.ID())
	if owner != "a" {
		t.Fatalf("expected owner 'a', got %q", owner)
	}

	// Peer should still be connected.
	if h.Network().Connectedness(remote.ID()) != network.Connected {
		t.Fatal("peer should remain connected when claimed by own instance")
	}
}

func TestHandleInboundPeers_RejectOtherOwner(t *testing.T) {
	ps := NewSharedPeerSet([]string{"a", "b"})
	h := newTestHost(t)
	remote := newTestHost(t)

	// Pre-assign remote to instance "b".
	ps.Claim(remote.ID(), "b")

	ctx := t.Context()
	go HandleInboundPeers(ctx, h, "a", ps)

	// Connect remote → h (inbound to h, but owned by "b").
	if err := remote.Connect(ctx, peer.AddrInfo{ID: h.ID(), Addrs: h.Addrs()}); err != nil {
		t.Fatal(err)
	}

	time.Sleep(200 * time.Millisecond)

	// Peer should be disconnected by HandleInboundPeers.
	if h.Network().Connectedness(remote.ID()) == network.Connected {
		t.Fatal("peer owned by other instance should be disconnected")
	}
}
