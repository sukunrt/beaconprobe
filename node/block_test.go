package node

import (
	"path/filepath"
	"testing"
	"time"

	"github.com/OffchainLabs/prysm/v7/consensus-types/primitives"
)

func TestBlockTopic(t *testing.T) {
	fd := [4]byte{0xab, 0xcd, 0xef, 0x01}
	got := BlockTopic(fd)
	want := "/eth2/abcdef01/beacon_block/ssz_snappy"
	if got != want {
		t.Fatalf("got %q, want %q", got, want)
	}
}

func TestBlockTracker_RecordAndGet(t *testing.T) {
	bt := NewBlockTracker()
	now := time.Now()

	bt.Record(100, now)

	got, ok := bt.GetBlockArrival(100)
	if !ok {
		t.Fatal("expected block to be recorded")
	}
	if got != now {
		t.Fatalf("expected %v, got %v", now, got)
	}
}

func TestBlockTracker_FirstArrivalWins(t *testing.T) {
	bt := NewBlockTracker()
	first := time.Now()
	second := first.Add(500 * time.Millisecond)

	bt.Record(100, first)
	bt.Record(100, second)

	got, ok := bt.GetBlockArrival(100)
	if !ok {
		t.Fatal("expected block to be recorded")
	}
	if got != first {
		t.Fatalf("expected first arrival %v, got %v", first, got)
	}
}

func TestBlockTracker_MissedBlock(t *testing.T) {
	bt := NewBlockTracker()

	// Slot 100 not recorded — should return not ok.
	_, ok := bt.GetBlockArrival(100)
	if ok {
		t.Fatal("expected no block for unrecorded slot")
	}
}

func TestBlockTracker_Cleanup(t *testing.T) {
	bt := NewBlockTracker()
	now := time.Now()

	bt.Record(10, now)
	bt.Record(50, now)
	bt.Record(100, now)

	bt.cleanup(60)

	// Slots 10 and 50 should be cleaned up.
	if _, ok := bt.GetBlockArrival(10); ok {
		t.Fatal("slot 10 should have been cleaned up")
	}
	if _, ok := bt.GetBlockArrival(50); ok {
		t.Fatal("slot 50 should have been cleaned up")
	}
	// Slot 100 should still be there.
	if _, ok := bt.GetBlockArrival(100); !ok {
		t.Fatal("slot 100 should not have been cleaned up")
	}
}

func TestBlockTracker_ConcurrentAccess(t *testing.T) {
	bt := NewBlockTracker()

	done := make(chan struct{})
	go func() {
		for i := range primitives.Slot(1000) {
			bt.Record(i, time.Now())
		}
		close(done)
	}()

	for i := range primitives.Slot(1000) {
		bt.GetBlockArrival(i)
	}
	<-done
}

func TestSubscribeBlocks(t *testing.T) {
	h := newTestHost(t)
	ctx := t.Context()

	genesisValRoot := make([]byte, 32)
	ps, err := NewGossipSub(ctx, h, genesisValRoot, 8, false, [4]byte{}, []uint64{0}, filepath.Join(t.TempDir(), "gossipsub.log"))
	if err != nil {
		t.Fatal(err)
	}

	fd := [4]byte{0x01, 0x02, 0x03, 0x04}
	sub, err := SubscribeBlocks(ps, fd)
	if err != nil {
		t.Fatal(err)
	}
	if sub == nil {
		t.Fatal("expected non-nil subscription")
	}
}
