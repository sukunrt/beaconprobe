package node

import (
	"context"
	"fmt"
	"log/slog"
	"time"

	"github.com/OffchainLabs/prysm/v7/beacon-chain/p2p/encoder"
	"github.com/OffchainLabs/prysm/v7/consensus-types/primitives"
	ethpb "github.com/OffchainLabs/prysm/v7/proto/prysm/v1alpha1"
	"github.com/OffchainLabs/prysm/v7/time/slots"
	"github.com/sukunrt/beaconprobe/metrics"
)

type bufferedAttestation struct {
	receiveTime time.Time
	subnetID    uint64
	slot        primitives.Slot
	attesterIdx primitives.ValidatorIndex
}

// ListenForAttestations buffers attestations per slot and logs them
// once the slot completes, so block arrival status is known.
func ListenForAttestations(ctx context.Context, subs []Subscription, genesisTime time.Time, blockTracker *BlockTracker, fileLogger *slog.Logger) {
	ch := make(chan bufferedAttestation, 4096)
	for _, s := range subs {
		go collectSubnet(ctx, s, ch)
	}
	go flushSlots(ctx, ch, genesisTime, blockTracker, fileLogger)
}

func collectSubnet(ctx context.Context, sub Subscription, ch chan<- bufferedAttestation) {
	enc := encoder.SszNetworkEncoder{}
	for {
		msg, err := sub.Sub.Next(ctx)
		if err != nil {
			if ctx.Err() != nil {
				return
			}
			slog.Error("error reading from subscription", "subnet", sub.SubnetID, "error", err)
			return
		}

		var att ethpb.SingleAttestation
		if err := enc.DecodeGossip(msg.Data, &att); err != nil {
			slog.Info("failed to decode attestation", "subnet", sub.SubnetID, "error", err)
			continue
		}

		ch <- bufferedAttestation{
			receiveTime: time.Now(),
			subnetID:    sub.SubnetID,
			slot:        att.Data.Slot,
			attesterIdx: att.AttesterIndex,
		}
	}
}

func nextSlotTimer(genesisTime time.Time) *time.Timer {
	currentSlot := slots.CurrentSlot(genesisTime)
	nextStart, _ := slots.StartTime(genesisTime, currentSlot+1)
	return time.NewTimer(time.Until(nextStart))
}

func flushSlots(ctx context.Context, ch <-chan bufferedAttestation, genesisTime time.Time, blockTracker *BlockTracker, fileLogger *slog.Logger) {
	pending := make(map[primitives.Slot][]bufferedAttestation)
	timer := nextSlotTimer(genesisTime)
	defer timer.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case entry := <-ch:
			pending[entry.slot] = append(pending[entry.slot], entry)
		case <-timer.C:
			completedSlot := slots.CurrentSlot(genesisTime) - 1
			for s, entries := range pending {
				if s <= completedSlot {
					go logSlotAttestations(s, entries, genesisTime, blockTracker, fileLogger)
					delete(pending, s)
				}
			}
			timer = nextSlotTimer(genesisTime)
		}
	}
}

func logSlotAttestations(slot primitives.Slot, entries []bufferedAttestation, genesisTime time.Time, blockTracker *BlockTracker, fileLogger *slog.Logger) {
	slotStart, err := slots.StartTime(genesisTime, slot)
	if err != nil {
		slog.Info("failed to compute slot start time", "slot", slot, "error", err)
		return
	}

	var blockStatus string
	var blockTime time.Time
	var blockTimeInSlot time.Duration
	if blockTracker != nil {
		if bt, ok := blockTracker.GetBlockArrival(slot); ok {
			blockStatus = "proposed"
			blockTime = bt
			blockTimeInSlot = bt.Sub(slotStart)
		} else {
			blockStatus = "missed"
			blockTime = slotStart.Add(4 * time.Second)
		}
	} else {
		blockStatus = "missed"
		blockTime = slotStart.Add(4 * time.Second)
	}

	for _, e := range entries {
		timeIntoSlot := e.receiveTime.Sub(slotStart)
		arrivalDelay := e.receiveTime.Sub(blockTime)
		subnetLabel := fmt.Sprintf("%d", e.subnetID)

		metrics.AttestationsReceived.WithLabelValues(subnetLabel).Inc()
		metrics.AttestationLatency.WithLabelValues(subnetLabel).Observe(arrivalDelay.Seconds())
		metrics.AttestationArrivalInSlot.WithLabelValues(subnetLabel).Observe(timeIntoSlot.Seconds())
		if timeIntoSlot > 8*time.Second {
			metrics.LateAttestations.WithLabelValues(subnetLabel).Inc()
		}

		attrs := []any{
			"subnet", e.subnetID,
			"slot", slot,
			"timeInSlot", timeIntoSlot.Round(time.Millisecond),
			"arrivalDelay", arrivalDelay.Round(time.Millisecond),
			"attester", e.attesterIdx,
			"block", blockStatus,
		}
		if blockStatus == "proposed" {
			attrs = append(attrs, "blockTimeInSlot", blockTimeInSlot.Round(time.Millisecond))
		}
		slog.Info("attestation", attrs...)

		if fileLogger != nil {
			fileAttrs := []any{
				"subnet", e.subnetID,
				"slot", slot,
				"timeInSlotMs", timeIntoSlot.Milliseconds(),
				"arrivalDelayMs", arrivalDelay.Milliseconds(),
				"attester", e.attesterIdx,
				"block", blockStatus,
			}
			if blockStatus == "proposed" {
				fileAttrs = append(fileAttrs, "blockTimeInSlotMs", blockTimeInSlot.Milliseconds())
			}
			fileLogger.Info("attestation", fileAttrs...)
		}
	}
}
