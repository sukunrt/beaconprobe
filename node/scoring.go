package node

import (
	"math"
	"net"
	"time"

	"github.com/OffchainLabs/prysm/v7/config/params"
	pubsub "github.com/libp2p/go-libp2p-pubsub"
	"github.com/libp2p/go-libp2p/core/peer"
)

const (
	// decayToZero is the terminal value for counter decay.
	decayToZero = 0.01

	// maxInMeshScore is the max score a peer can attain from being in the mesh.
	maxInMeshScore = 10
	// maxFirstDeliveryScore is the max score a peer can obtain from first deliveries.
	maxFirstDeliveryScore = 40
)

func oneSlotDuration() time.Duration {
	return time.Duration(params.BeaconConfig().SecondsPerSlot) * time.Second
}

func oneEpochDuration() time.Duration {
	return time.Duration(params.BeaconConfig().SlotsPerEpoch) * oneSlotDuration()
}

// scoreDecay computes a per-slot decay factor such that a value decays to
// decayToZero over totalDuration.
func scoreDecay(totalDuration time.Duration) float64 {
	numSlots := totalDuration / oneSlotDuration()
	return math.Pow(decayToZero, 1/float64(numSlots))
}

// scoreByWeight computes the penalty needed so that a peer at the threshold
// deficit reaches -maxScore when scaled by weight.
func scoreByWeight(weight, threshold float64) float64 {
	maxScore := float64(maxInMeshScore+maxFirstDeliveryScore) * weight
	return maxScore / (weight * threshold * threshold)
}

// inMeshCap returns the cap for time-in-mesh scoring (1 hour worth of slots).
func inMeshCap() float64 {
	return float64((3600 * time.Second) / oneSlotDuration())
}

// PeerScoreConfig returns the peer-level and threshold scoring parameters.
func PeerScoreConfig(topicScores map[string]*pubsub.TopicScoreParams) (*pubsub.PeerScoreParams, *pubsub.PeerScoreThresholds) {
	scoreParams := &pubsub.PeerScoreParams{
		SkipAtomicValidation: true,
		Topics:               topicScores,
		TopicScoreCap:        32.72,
		AppSpecificScore:     func(peer.ID) float64 { return 0 },
		AppSpecificWeight:    1,
		// Disable IP colocation penalty — we're a probe, not a validator.
		IPColocationFactorWeight:    0,
		IPColocationFactorThreshold: 10,
		IPColocationFactorWhitelist: []*net.IPNet{},
		BehaviourPenaltyWeight:      -15.92,
		BehaviourPenaltyThreshold:   6,
		BehaviourPenaltyDecay:       scoreDecay(10 * oneEpochDuration()),
		DecayInterval:               oneSlotDuration(),
		DecayToZero:                 decayToZero,
		RetainScore:                 100 * oneEpochDuration(),
	}

	thresholds := &pubsub.PeerScoreThresholds{
		GossipThreshold:             -4000,
		PublishThreshold:            -8000,
		GraylistThreshold:           -16000,
		AcceptPXThreshold:           100,
		OpportunisticGraftThreshold: 5,
	}

	return scoreParams, thresholds
}

// AttestationTopicScoreParams returns topic scoring params for attestation subnets.
func AttestationTopicScoreParams() *pubsub.TopicScoreParams {
	epochDuration := oneEpochDuration()
	return &pubsub.TopicScoreParams{
		SkipAtomicValidation: true,
		TopicWeight:          1,

		// P1: time in mesh — small positive reward for staying grafted.
		TimeInMeshWeight:  maxInMeshScore / inMeshCap(),
		TimeInMeshQuantum: oneSlotDuration(),
		TimeInMeshCap:     inMeshCap(),

		// P2: first message deliveries — the main signal.
		// Score increases on each new message, decays over 1 epoch.
		FirstMessageDeliveriesWeight: 1,
		FirstMessageDeliveriesDecay:  scoreDecay(1 * epochDuration),
		FirstMessageDeliveriesCap:    maxFirstDeliveryScore,

		// P3: mesh message deliveries — penalize peers that don't deliver.
		MeshMessageDeliveriesWeight:     -scoreByWeight(1, 5),
		MeshMessageDeliveriesDecay:      scoreDecay(4 * epochDuration),
		MeshMessageDeliveriesCap:        20,
		MeshMessageDeliveriesThreshold:  5,
		MeshMessageDeliveriesWindow:     2 * time.Second,
		MeshMessageDeliveriesActivation: 1 * epochDuration,
		MeshFailurePenaltyWeight:        -scoreByWeight(1, 5),
		MeshFailurePenaltyDecay:         scoreDecay(4 * epochDuration),

		// P4: invalid messages — disabled (we don't validate).
		InvalidMessageDeliveriesWeight: 0,
		InvalidMessageDeliveriesDecay:  0,
	}
}
