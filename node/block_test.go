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
	genesisTime := time.Unix(1606824023, 0)
	now := time.Now()

	bt.Record(100, now)

	got := bt.GetBlockTime(100, genesisTime)
	if got != now {
		t.Fatalf("expected %v, got %v", now, got)
	}
}

func TestBlockTracker_FirstArrivalWins(t *testing.T) {
	bt := NewBlockTracker()
	genesisTime := time.Unix(1606824023, 0)
	first := time.Now()
	second := first.Add(500 * time.Millisecond)

	bt.Record(100, first)
	bt.Record(100, second)

	got := bt.GetBlockTime(100, genesisTime)
	if got != first {
		t.Fatalf("expected first arrival %v, got %v", first, got)
	}
}

func TestBlockTracker_FallbackToSlotStart(t *testing.T) {
	bt := NewBlockTracker()
	genesisTime := time.Unix(1606824023, 0)

	// Slot 100 not recorded — should return slot_start + 4s.
	got := bt.GetBlockTime(100, genesisTime)
	wantSlotStart := genesisTime.Add(time.Duration(100*12) * time.Second)
	want := wantSlotStart.Add(4 * time.Second)
	if got != want {
		t.Fatalf("expected fallback %v, got %v", want, got)
	}
}

func TestBlockTracker_Cleanup(t *testing.T) {
	bt := NewBlockTracker()
	genesisTime := time.Unix(1606824023, 0)
	now := time.Now()

	bt.Record(10, now)
	bt.Record(50, now)
	bt.Record(100, now)

	bt.cleanup(60)

	// Slots 10 and 50 should be cleaned up.
	if got := bt.GetBlockTime(10, genesisTime); got == now {
		t.Fatal("slot 10 should have been cleaned up")
	}
	if got := bt.GetBlockTime(50, genesisTime); got == now {
		t.Fatal("slot 50 should have been cleaned up")
	}
	// Slot 100 should still be there.
	if got := bt.GetBlockTime(100, genesisTime); got != now {
		t.Fatal("slot 100 should not have been cleaned up")
	}
}

func TestBlockTracker_ConcurrentAccess(t *testing.T) {
	bt := NewBlockTracker()
	genesisTime := time.Unix(1606824023, 0)

	done := make(chan struct{})
	go func() {
		for i := primitives.Slot(0); i < 1000; i++ {
			bt.Record(i, time.Now())
		}
		close(done)
	}()

	for i := primitives.Slot(0); i < 1000; i++ {
		bt.GetBlockTime(i, genesisTime)
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
