package node

import (
	"context"
	"testing"
	"time"

	"github.com/libp2p/go-libp2p/core/peer"
)

func TestBackoff_ExponentialIncrease(t *testing.T) {
	h := newTestHost(t)
	pm := NewPeerManager(h, 8)

	id := h.ID() // use own ID as dummy

	// First failure: 30s backoff.
	pm.recordFailure(id)
	d := pm.BackoffDuration(id)
	if d < 29*time.Second || d > 31*time.Second {
		t.Fatalf("expected ~30s backoff after 1 failure, got %v", d)
	}

	// Second failure: 60s backoff.
	pm.recordFailure(id)
	d = pm.BackoffDuration(id)
	if d < 59*time.Second || d > 61*time.Second {
		t.Fatalf("expected ~60s backoff after 2 failures, got %v", d)
	}

	// Third failure: 120s backoff.
	pm.recordFailure(id)
	d = pm.BackoffDuration(id)
	if d < 119*time.Second || d > 121*time.Second {
		t.Fatalf("expected ~120s backoff after 3 failures, got %v", d)
	}
}

func TestBackoff_CapsAtMax(t *testing.T) {
	h := newTestHost(t)
	pm := NewPeerManager(h, 8)

	id := h.ID()

	// Apply many failures to exceed max backoff.
	for range 20 {
		pm.recordFailure(id)
	}

	d := pm.BackoffDuration(id)
	if d > maxBackoff+time.Second {
		t.Fatalf("backoff %v exceeds max %v", d, maxBackoff)
	}
}

func TestBackoff_ClearOnSuccess(t *testing.T) {
	h := newTestHost(t)
	pm := NewPeerManager(h, 8)

	id := h.ID()

	pm.recordFailure(id)
	if !pm.inBackoff(id) {
		t.Fatal("expected peer to be in backoff after failure")
	}

	pm.clearBackoff(id)
	if pm.inBackoff(id) {
		t.Fatal("expected backoff to be cleared")
	}
	if d := pm.BackoffDuration(id); d != 0 {
		t.Fatalf("expected 0 backoff duration after clear, got %v", d)
	}
}

func TestBackoff_ExpiredBackoffNotBlocking(t *testing.T) {
	h := newTestHost(t)
	pm := NewPeerManager(h, 8)

	id := h.ID()

	// Manually set a backoff that's already expired.
	pm.mu.Lock()
	pm.backoff[id] = backoffEntry{
		failCount: 1,
		nextDial:  time.Now().Add(-time.Second),
	}
	pm.mu.Unlock()

	if pm.inBackoff(id) {
		t.Fatal("expired backoff should not block")
	}
}

func TestPeerManager_SkipsAtCap(t *testing.T) {
	h := newTestHost(t)
	pm := NewPeerManager(h, 1) // maxPeers = 10

	// Create 10 connected peers by connecting test hosts.
	peers := make([]peer.AddrInfo, 10)
	for i := range 10 {
		ph := newTestHost(t)
		peers[i] = peer.AddrInfo{ID: ph.ID(), Addrs: ph.Addrs()}
		if err := h.Connect(t.Context(), peers[i]); err != nil {
			t.Fatal(err)
		}
	}

	// Now at cap (10). Send a candidate — it should be skipped (not dialed).
	extra := newTestHost(t)
	ctx, cancel := context.WithTimeout(t.Context(), 100*time.Millisecond)
	defer cancel()
	pm.handleCandidate(ctx, peer.AddrInfo{ID: extra.ID(), Addrs: extra.Addrs()})

	// The extra peer should not be connected.
	conns := h.Network().ConnsToPeer(extra.ID())
	if len(conns) > 0 {
		t.Fatal("peer manager should not dial when at cap")
	}
}

func TestPeerManager_DialsBelowCap(t *testing.T) {
	h := newTestHost(t)
	pm := NewPeerManager(h, 1) // maxPeers = 10, no peers connected

	target := newTestHost(t)
	pm.handleCandidate(t.Context(), peer.AddrInfo{ID: target.ID(), Addrs: target.Addrs()})

	conns := h.Network().ConnsToPeer(target.ID())
	if len(conns) == 0 {
		t.Fatal("peer manager should dial when below cap")
	}
}
