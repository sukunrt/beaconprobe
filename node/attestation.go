package node

import (
	"context"
	"fmt"
	"log/slog"
	"time"

	"github.com/OffchainLabs/prysm/v7/beacon-chain/p2p/encoder"
	ethpb "github.com/OffchainLabs/prysm/v7/proto/prysm/v1alpha1"
	"github.com/OffchainLabs/prysm/v7/time/slots"
	"github.com/sukunrt/beaconprobe/metrics"
)

// ListenForAttestations starts a goroutine for each subscription that processes attestation messages.
// If fileLogger is non-nil, attestation arrival data is written to it.
func ListenForAttestations(ctx context.Context, subs []Subscription, genesisTime time.Time, fileLogger *slog.Logger) {
	for _, s := range subs {
		go listenSubnet(ctx, s, genesisTime, fileLogger)
	}
}

func listenSubnet(ctx context.Context, sub Subscription, genesisTime time.Time, fileLogger *slog.Logger) {
	subnetLabel := fmt.Sprintf("%d", sub.SubnetID)
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

		receiveTime := time.Now()

		var att ethpb.SingleAttestation
		if err := enc.DecodeGossip(msg.Data, &att); err != nil {
			slog.Debug("failed to decode attestation", "subnet", sub.SubnetID, "error", err)
			continue
		}

		slotStart, err := slots.StartTime(genesisTime, att.Data.Slot)
		if err != nil {
			slog.Debug("failed to compute slot start time", "error", err)
			continue
		}

		timeIntoSlot := receiveTime.Sub(slotStart)
		arrivalDelay := timeIntoSlot - 4*time.Second

		metrics.AttestationsReceived.WithLabelValues(subnetLabel).Inc()
		metrics.AttestationLatency.WithLabelValues(subnetLabel).Observe(arrivalDelay.Seconds())
		metrics.AttestationArrivalInSlot.WithLabelValues(subnetLabel).Observe(timeIntoSlot.Seconds())

		if timeIntoSlot > 8*time.Second {
			metrics.LateAttestations.WithLabelValues(subnetLabel).Inc()
		}

		slog.Debug("attestation received",
			"subnet", sub.SubnetID,
			"slot", att.Data.Slot,
			"timeInSlot", timeIntoSlot.Round(time.Millisecond),
			"arrivalDelay", arrivalDelay.Round(time.Millisecond),
			"attester", att.AttesterIndex,
		)

		if fileLogger != nil {
			fileLogger.Info("attestation",
				"subnet", sub.SubnetID,
				"slot", att.Data.Slot,
				"timeInSlotMs", timeIntoSlot.Milliseconds(),
				"arrivalDelayMs", arrivalDelay.Milliseconds(),
				"attester", att.AttesterIndex,
			)
		}
	}
}
