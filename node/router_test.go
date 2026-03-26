package node

import (
	"testing"

	"github.com/libp2p/go-libp2p/core/host"
	"github.com/libp2p/go-libp2p/core/peer"
)

func TestMultiInstanceRouter_Route(t *testing.T) {
	ps := NewSharedPeerSet([]string{"a", "b"})
	chA := make(chan peer.AddrInfo, 10)
	chB := make(chan peer.AddrInfo, 10)
	hA := newTestHost(t)
	hB := newTestHost(t)

	router := NewMultiInstanceRouter(
		ps,
		map[string]chan<- peer.AddrInfo{"a": chA, "b": chB},
		map[string]host.Host{"a": hA, "b": hB},
	)

	// Route 10 candidates — should be roughly evenly split.
	peers := make([]peer.ID, 10)
	for i := range 10 {
		p := newTestHost(t)
		peers[i] = p.ID()
		router.Route(peer.AddrInfo{ID: p.ID(), Addrs: p.Addrs()})
	}

	gotA := len(chA)
	gotB := len(chB)
	if gotA+gotB != 10 {
		t.Fatalf("expected 10 routed peers, got %d+%d=%d", gotA, gotB, gotA+gotB)
	}
	if gotA == 0 || gotB == 0 {
		t.Fatalf("expected both instances to get peers, got a=%d b=%d", gotA, gotB)
	}
}

func TestMultiInstanceRouter_IsConnectedToAny(t *testing.T) {
	hA := newTestHost(t)
	hB := newTestHost(t)
	remote := newTestHost(t)

	ps := NewSharedPeerSet([]string{"a", "b"})
	router := NewMultiInstanceRouter(
		ps,
		map[string]chan<- peer.AddrInfo{"a": make(chan peer.AddrInfo, 1), "b": make(chan peer.AddrInfo, 1)},
		map[string]host.Host{"a": hA, "b": hB},
	)

	if router.IsConnectedToAny(remote.ID()) {
		t.Fatal("should not be connected before dialing")
	}

	// Connect hA to remote.
	if err := hA.Connect(t.Context(), peer.AddrInfo{ID: remote.ID(), Addrs: remote.Addrs()}); err != nil {
		t.Fatal(err)
	}

	if !router.IsConnectedToAny(remote.ID()) {
		t.Fatal("should be connected after dialing from hA")
	}
}

func TestMultiInstanceRouter_StickyAssignment(t *testing.T) {
	ps := NewSharedPeerSet([]string{"a", "b"})
	chA := make(chan peer.AddrInfo, 10)
	chB := make(chan peer.AddrInfo, 10)

	router := NewMultiInstanceRouter(
		ps,
		map[string]chan<- peer.AddrInfo{"a": chA, "b": chB},
		map[string]host.Host{"a": newTestHost(t), "b": newTestHost(t)},
	)

	p := newTestHost(t)
	ai := peer.AddrInfo{ID: p.ID(), Addrs: p.Addrs()}

	// Route same peer twice — should go to the same instance.
	router.Route(ai)
	router.Route(ai)

	gotA := len(chA)
	gotB := len(chB)
	// Both entries should be in the same channel.
	if gotA == 2 && gotB == 0 {
		return // ok
	}
	if gotA == 0 && gotB == 2 {
		return // ok
	}
	t.Fatalf("same peer should be routed to same instance, got a=%d b=%d", gotA, gotB)
}

func TestMultiInstanceRouter_SingleInstance(t *testing.T) {
	ps := NewSharedPeerSet([]string{"solo"})
	ch := make(chan peer.AddrInfo, 10)

	router := NewMultiInstanceRouter(
		ps,
		map[string]chan<- peer.AddrInfo{"solo": ch},
		map[string]host.Host{"solo": newTestHost(t)},
	)

	for range 5 {
		p := newTestHost(t)
		router.Route(peer.AddrInfo{ID: p.ID(), Addrs: p.Addrs()})
	}

	if len(ch) != 5 {
		t.Fatalf("expected 5 routed peers, got %d", len(ch))
	}
}
