package node

import (
	"sync"
	"testing"

	"github.com/libp2p/go-libp2p/core/host"
	"github.com/libp2p/go-libp2p/core/peer"
)

func newPeerFinder(t *testing.T, names []string) (*PeerFinder, map[string]host.Host) {
	t.Helper()
	ps := NewSharedPeerSet(names)
	hosts := make(map[string]host.Host, len(names))
	for _, name := range names {
		hosts[name] = newTestHost(t)
	}
	return NewPeerFinder(ps, hosts), hosts
}

func TestPeerFinder_AddAndGetPeers(t *testing.T) {
	pf, _ := newPeerFinder(t, []string{"a", "b"})

	for range 10 {
		p := newTestHost(t)
		pf.AddPeer(peer.AddrInfo{ID: p.ID(), Addrs: p.Addrs()})
	}

	gotA := pf.GetPeers("a", 100, nil)
	gotB := pf.GetPeers("b", 100, nil)
	total := len(gotA) + len(gotB)
	if total != 10 {
		t.Fatalf("expected 10 total peers, got %d (a=%d b=%d)", total, len(gotA), len(gotB))
	}
	if len(gotA) == 0 || len(gotB) == 0 {
		t.Fatalf("expected both instances to get peers, got a=%d b=%d", len(gotA), len(gotB))
	}
}

func TestPeerFinder_NeverDrops(t *testing.T) {
	pf, _ := newPeerFinder(t, []string{"solo"})

	// Add more peers than the old channel capacity (64).
	const n = 200
	for range n {
		p := newTestHost(t)
		pf.AddPeer(peer.AddrInfo{ID: p.ID(), Addrs: p.Addrs()})
	}

	got := pf.GetPeers("solo", n+100, nil)
	if len(got) != n {
		t.Fatalf("expected %d peers, got %d", n, len(got))
	}
}

func TestPeerFinder_GetPeersCountLimit(t *testing.T) {
	pf, _ := newPeerFinder(t, []string{"solo"})

	for range 20 {
		p := newTestHost(t)
		pf.AddPeer(peer.AddrInfo{ID: p.ID(), Addrs: p.Addrs()})
	}

	got := pf.GetPeers("solo", 5, nil)
	if len(got) != 5 {
		t.Fatalf("expected 5 peers, got %d", len(got))
	}
}

func TestPeerFinder_SkipFunction(t *testing.T) {
	pf, _ := newPeerFinder(t, []string{"solo"})

	var ids []peer.ID
	for range 10 {
		p := newTestHost(t)
		ids = append(ids, p.ID())
		pf.AddPeer(peer.AddrInfo{ID: p.ID(), Addrs: p.Addrs()})
	}

	// Skip the first 5 peers.
	skipSet := make(map[peer.ID]struct{})
	for _, id := range ids[:5] {
		skipSet[id] = struct{}{}
	}
	skip := func(id peer.ID) bool {
		_, ok := skipSet[id]
		return ok
	}

	got := pf.GetPeers("solo", 100, skip)
	if len(got) != 5 {
		t.Fatalf("expected 5 non-skipped peers, got %d", len(got))
	}
	for _, ai := range got {
		if _, ok := skipSet[ai.ID]; ok {
			t.Fatalf("skipped peer %s should not be returned", ai.ID)
		}
	}
}

func TestPeerFinder_StickyAssignment(t *testing.T) {
	pf, _ := newPeerFinder(t, []string{"a", "b"})

	p := newTestHost(t)
	ai := peer.AddrInfo{ID: p.ID(), Addrs: p.Addrs()}

	pf.AddPeer(ai)
	pf.AddPeer(ai) // re-add same peer

	gotA := pf.GetPeers("a", 100, nil)
	gotB := pf.GetPeers("b", 100, nil)

	// Peer should appear in exactly one instance, exactly once.
	if len(gotA)+len(gotB) != 1 {
		t.Fatalf("expected 1 total peer, got a=%d b=%d", len(gotA), len(gotB))
	}
}

func TestPeerFinder_IsConnectedToAny(t *testing.T) {
	pf, hosts := newPeerFinder(t, []string{"a", "b"})
	remote := newTestHost(t)

	if pf.IsConnectedToAny(remote.ID()) {
		t.Fatal("should not be connected before dialing")
	}

	hA := hosts["a"]
	if err := hA.Connect(t.Context(), peer.AddrInfo{ID: remote.ID(), Addrs: remote.Addrs()}); err != nil {
		t.Fatal(err)
	}

	if !pf.IsConnectedToAny(remote.ID()) {
		t.Fatal("should be connected after dialing from hA")
	}
}

func TestPeerFinder_SingleInstance(t *testing.T) {
	pf, _ := newPeerFinder(t, []string{"solo"})

	for range 5 {
		p := newTestHost(t)
		pf.AddPeer(peer.AddrInfo{ID: p.ID(), Addrs: p.Addrs()})
	}

	got := pf.GetPeers("solo", 100, nil)
	if len(got) != 5 {
		t.Fatalf("expected 5 peers, got %d", len(got))
	}
}

func TestPeerFinder_UnknownInstance(t *testing.T) {
	pf, _ := newPeerFinder(t, []string{"a"})

	got := pf.GetPeers("nonexistent", 10, nil)
	if got != nil {
		t.Fatalf("expected nil for unknown instance, got %d peers", len(got))
	}
}

func TestPeerFinder_Route(t *testing.T) {
	pf, _ := newPeerFinder(t, []string{"solo"})

	p := newTestHost(t)
	pf.Route(peer.AddrInfo{ID: p.ID(), Addrs: p.Addrs()})

	got := pf.GetPeers("solo", 10, nil)
	if len(got) != 1 {
		t.Fatalf("expected 1 peer via Route, got %d", len(got))
	}
}

func TestPeerFinder_ConcurrentAddAndGet(t *testing.T) {
	pf, _ := newPeerFinder(t, []string{"solo"})

	var wg sync.WaitGroup
	// Concurrent adds.
	for range 50 {
		wg.Go(func() {
			p := newTestHost(t)
			pf.AddPeer(peer.AddrInfo{ID: p.ID(), Addrs: p.Addrs()})
		})
	}
	// Concurrent gets.
	for range 50 {
		wg.Go(func() {
			pf.GetPeers("solo", 10, nil)
		})
	}
	wg.Wait()

	got := pf.GetPeers("solo", 100, nil)
	if len(got) != 50 {
		t.Fatalf("expected 50 peers after concurrent adds, got %d", len(got))
	}
}

func TestPeerFinder_GetPeersEmpty(t *testing.T) {
	pf, _ := newPeerFinder(t, []string{"solo"})

	got := pf.GetPeers("solo", 10, nil)
	if got != nil {
		t.Fatalf("expected nil for empty instance, got %d peers", len(got))
	}
}

func TestPeerFinder_SkipAll(t *testing.T) {
	pf, _ := newPeerFinder(t, []string{"solo"})

	for range 5 {
		p := newTestHost(t)
		pf.AddPeer(peer.AddrInfo{ID: p.ID(), Addrs: p.Addrs()})
	}

	got := pf.GetPeers("solo", 10, func(peer.ID) bool { return true })
	if got != nil {
		t.Fatalf("expected nil when all peers are skipped, got %d", len(got))
	}
}
