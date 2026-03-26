package node

import (
	"context"
	"log/slog"
	"sync"
	"time"

	"github.com/OffchainLabs/prysm/v7/beacon-chain/p2p/encoder"
	"github.com/OffchainLabs/prysm/v7/consensus-types/primitives"
	ethpb "github.com/OffchainLabs/prysm/v7/proto/prysm/v1alpha1"
	"github.com/OffchainLabs/prysm/v7/time/slots"
	pubsub "github.com/libp2p/go-libp2p-pubsub"

	"github.com/sukunrt/beaconprobe/metrics"
)

// BlockTracker records block arrival times per slot.
type BlockTracker struct {
	mu     sync.Mutex
	blocks map[primitives.Slot]time.Time
}

func NewBlockTracker() *BlockTracker {
	return &BlockTracker{
		blocks: make(map[primitives.Slot]time.Time),
	}
}

// Record stores the first block arrival time for a slot.
func (bt *BlockTracker) Record(slot primitives.Slot, t time.Time) {
	bt.mu.Lock()
	defer bt.mu.Unlock()
	if _, ok := bt.blocks[slot]; !ok {
		bt.blocks[slot] = t
	}
}

// GetBlockArrival returns the block arrival time for a slot and whether a block was received.
func (bt *BlockTracker) GetBlockArrival(slot primitives.Slot) (time.Time, bool) {
	bt.mu.Lock()
	t, ok := bt.blocks[slot]
	bt.mu.Unlock()
	return t, ok
}

// cleanup removes entries older than the given slot.
func (bt *BlockTracker) cleanup(minSlot primitives.Slot) {
	bt.mu.Lock()
	defer bt.mu.Unlock()
	for s := range bt.blocks {
		if s < minSlot {
			delete(bt.blocks, s)
		}
	}
}

// ListenForBlocks listens for beacon block messages and records their arrival times.
func ListenForBlocks(ctx context.Context, sub *pubsub.Subscription, genesisTime time.Time, tracker *BlockTracker, fileLogger *slog.Logger, m *metrics.Metrics) {
	enc := encoder.SszNetworkEncoder{}
	var lastCleanup primitives.Slot

	for {
		msg, err := sub.Next(ctx)
		if err != nil {
			if ctx.Err() != nil {
				return
			}
			slog.Error("error reading from block subscription", "error", err)
			return
		}

		receiveTime := time.Now()

		var block ethpb.SignedBeaconBlockFulu
		if err := enc.DecodeGossip(msg.Data, &block); err != nil {
			slog.Info("failed to decode beacon block", "error", err)
			continue
		}

		slot := block.Block.Slot
		slotStart, err := slots.StartTime(genesisTime, slot)
		if err != nil {
			slog.Info("failed to compute slot start time for block", "error", err)
			continue
		}

		timeIntoSlot := receiveTime.Sub(slotStart)

		tracker.Record(slot, receiveTime)
		m.BlocksReceived.Inc()
		m.BlockArrivalInSlot.Observe(timeIntoSlot.Seconds())

		slog.Info("block received",
			"slot", slot,
			"timeInSlot", timeIntoSlot.Round(time.Millisecond),
		)

		if fileLogger != nil {
			fileLogger.Info("block",
				"slot", slot,
				"timeInSlotMs", timeIntoSlot.Milliseconds(),
			)
		}

		// Cleanup old entries every 64 slots (~2 epochs).
		if slot-lastCleanup > 64 {
			lastCleanup = slot
			tracker.cleanup(slot - 64)
		}
	}
}
