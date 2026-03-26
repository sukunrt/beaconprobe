package node

import (
	"sync"
	"testing"

	"github.com/libp2p/go-libp2p/core/crypto"
	"github.com/libp2p/go-libp2p/core/peer"
)

func generatePeerID(t *testing.T) peer.ID {
	t.Helper()
	priv, _, err := crypto.GenerateKeyPair(crypto.Ed25519, 0)
	if err != nil {
		t.Fatal(err)
	}
	id, err := peer.IDFromPrivateKey(priv)
	if err != nil {
		t.Fatal(err)
	}
	return id
}

func TestSharedPeerSetAssignOrGet(t *testing.T) {
	ps := NewSharedPeerSet([]string{"a", "b"})
	p1 := generatePeerID(t)
	p2 := generatePeerID(t)

	owner1 := ps.AssignOrGet(p1)
	owner2 := ps.AssignOrGet(p2)

	// Round-robin: first goes to "a", second to "b".
	if owner1 != "a" {
		t.Fatalf("peer1 owner = %q, want a", owner1)
	}
	if owner2 != "b" {
		t.Fatalf("peer2 owner = %q, want b", owner2)
	}

	// Re-query returns same owner (idempotent).
	if got := ps.AssignOrGet(p1); got != "a" {
		t.Fatalf("peer1 re-query = %q, want a", got)
	}
}

func TestSharedPeerSetOwner(t *testing.T) {
	ps := NewSharedPeerSet([]string{"a", "b"})
	p := generatePeerID(t)

	if got := ps.Owner(p); got != "" {
		t.Fatalf("unassigned peer owner = %q, want empty", got)
	}

	ps.AssignOrGet(p)
	if got := ps.Owner(p); got != "a" {
		t.Fatalf("assigned peer owner = %q, want a", got)
	}
}

func TestSharedPeerSetClaim(t *testing.T) {
	ps := NewSharedPeerSet([]string{"a", "b"})
	p := generatePeerID(t)

	// Claim unassigned peer.
	owner, ok := ps.Claim(p, "b")
	if owner != "b" || !ok {
		t.Fatalf("Claim unassigned = (%q, %v), want (b, true)", owner, ok)
	}

	// Claim again by same instance — succeeds.
	owner, ok = ps.Claim(p, "b")
	if owner != "b" || !ok {
		t.Fatalf("Claim same = (%q, %v), want (b, true)", owner, ok)
	}

	// Claim by different instance — fails.
	owner, ok = ps.Claim(p, "a")
	if owner != "b" || ok {
		t.Fatalf("Claim different = (%q, %v), want (b, false)", owner, ok)
	}
}

func TestSharedPeerSetRoundRobinBalance(t *testing.T) {
	instances := []string{"a", "b", "c"}
	ps := NewSharedPeerSet(instances)

	counts := make(map[string]int)
	n := 30
	for range n {
		p := generatePeerID(t)
		owner := ps.AssignOrGet(p)
		counts[owner]++
	}

	for _, name := range instances {
		if counts[name] != n/len(instances) {
			t.Fatalf("instance %q got %d peers, want %d", name, counts[name], n/len(instances))
		}
	}
}

func TestSharedPeerSetConcurrent(t *testing.T) {
	ps := NewSharedPeerSet([]string{"a", "b"})

	// Pre-generate peers to avoid t.Fatal in goroutines.
	const numPeers = 100
	peers := make([]peer.ID, numPeers)
	for i := range peers {
		peers[i] = generatePeerID(t)
	}

	var wg sync.WaitGroup
	results := make([]string, numPeers)

	// Multiple goroutines assign the same peers concurrently.
	for i := range numPeers {
		wg.Add(2)
		i := i
		go func() {
			defer wg.Done()
			results[i] = ps.AssignOrGet(peers[i])
		}()
		go func() {
			defer wg.Done()
			ps.AssignOrGet(peers[i])
		}()
	}
	wg.Wait()

	// Verify consistency: re-querying returns the same owner.
	for i, p := range peers {
		if got := ps.Owner(p); got != results[i] {
			t.Errorf("peer %d: Owner=%q, initial=%q", i, got, results[i])
		}
	}
}

func TestSharedPeerSetClaimAfterAssign(t *testing.T) {
	ps := NewSharedPeerSet([]string{"a", "b"})
	p := generatePeerID(t)

	// AssignOrGet assigns to "a".
	ps.AssignOrGet(p)

	// Claim by "b" should fail.
	owner, ok := ps.Claim(p, "b")
	if owner != "a" || ok {
		t.Fatalf("Claim after AssignOrGet = (%q, %v), want (a, false)", owner, ok)
	}
}

func TestSharedPeerSetPersistenceAcrossDisconnect(t *testing.T) {
	ps := NewSharedPeerSet([]string{"a", "b"})
	p := generatePeerID(t)

	// Assign peer.
	owner := ps.AssignOrGet(p)
	if owner != "a" {
		t.Fatalf("initial owner = %q, want a", owner)
	}

	// Simulate disconnect (no-op — there's nothing to do).
	// Re-query: peer is still assigned to "a".
	if got := ps.Owner(p); got != "a" {
		t.Fatalf("after disconnect, owner = %q, want a", got)
	}

	// New peer should go to "b" (round-robin continues).
	p2 := generatePeerID(t)
	if got := ps.AssignOrGet(p2); got != "b" {
		t.Fatalf("second peer owner = %q, want b", got)
	}

	// Re-discover original peer: still goes to "a".
	if got := ps.AssignOrGet(p); got != "a" {
		t.Fatalf("re-discovered peer owner = %q, want a", got)
	}
}

func BenchmarkAssignOrGet(b *testing.B) {
	ps := NewSharedPeerSet([]string{"a", "b", "c"})
	peers := make([]peer.ID, 1000)
	for i := range peers {
		priv, _, _ := crypto.GenerateKeyPair(crypto.Ed25519, 0)
		id, _ := peer.IDFromPrivateKey(priv)
		peers[i] = id
	}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		ps.AssignOrGet(peers[i%len(peers)])
	}
}

func BenchmarkAssignOrGetParallel(b *testing.B) {
	ps := NewSharedPeerSet([]string{"a", "b", "c"})
	peers := make([]peer.ID, 1000)
	for i := range peers {
		priv, _, _ := crypto.GenerateKeyPair(crypto.Ed25519, 0)
		id, _ := peer.IDFromPrivateKey(priv)
		peers[i] = id
	}

	b.ResetTimer()
	b.RunParallel(func(pb *testing.PB) {
		i := 0
		for pb.Next() {
			ps.AssignOrGet(peers[i%len(peers)])
			i++
		}
	})
}

func TestSharedPeerSetClaimConcurrent(t *testing.T) {
	ps := NewSharedPeerSet([]string{"a", "b"})
	p := generatePeerID(t)

	var wg sync.WaitGroup
	winners := make(chan string, 2)

	for _, inst := range []string{"a", "b"} {
		wg.Add(1)
		go func(name string) {
			defer wg.Done()
			_, ok := ps.Claim(p, name)
			if ok {
				winners <- name
			}
		}(inst)
	}

	wg.Wait()
	close(winners)

	// Exactly one should win.
	var winCount int
	var winner string
	for w := range winners {
		winCount++
		winner = w
	}
	if winCount != 1 {
		t.Fatalf("expected 1 winner, got %d", winCount)
	}

	// Owner should match the winner.
	if got := ps.Owner(p); got != winner {
		t.Fatalf("Owner = %q, winner = %q", got, winner)
	}
}

func TestSharedPeerSetRoundRobinWraps(t *testing.T) {
	ps := NewSharedPeerSet([]string{"x", "y"})

	expected := []string{"x", "y", "x", "y", "x"}
	for i, want := range expected {
		p := generatePeerID(t)
		got := ps.AssignOrGet(p)
		if got != want {
			t.Fatalf("peer %d: owner = %q, want %q", i, got, want)
		}
	}
}

func TestNewSharedPeerSetPanicsOnEmpty(t *testing.T) {
	defer func() {
		if r := recover(); r == nil {
			t.Fatal("expected panic for empty instance names")
		}
	}()
	NewSharedPeerSet([]string{})
}
