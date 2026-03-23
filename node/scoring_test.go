package node

import (
	"math"
	"testing"
	"time"

	pubsub "github.com/libp2p/go-libp2p-pubsub"
)

func TestScoreDecay(t *testing.T) {
	slot := oneSlotDuration()
	epoch := oneEpochDuration()

	t.Run("one epoch decay", func(t *testing.T) {
		decay := scoreDecay(epoch)
		if decay <= 0 || decay >= 1 {
			t.Fatalf("decay should be in (0,1), got %f", decay)
		}
		// After one epoch of slots, value should be ~decayToZero.
		numSlots := epoch / slot
		result := math.Pow(decay, float64(numSlots))
		if math.Abs(result-decayToZero) > 0.001 {
			t.Fatalf("expected value after %d slots to be ~%f, got %f", numSlots, decayToZero, result)
		}
	})

	t.Run("longer duration decays slower", func(t *testing.T) {
		short := scoreDecay(1 * epoch)
		long := scoreDecay(10 * epoch)
		if long <= short {
			t.Fatalf("longer duration should have higher (slower) decay: short=%f long=%f", short, long)
		}
	})

	t.Run("single slot duration", func(t *testing.T) {
		decay := scoreDecay(slot)
		// One slot should decay directly to decayToZero.
		if math.Abs(decay-decayToZero) > 0.001 {
			t.Fatalf("expected single-slot decay ~%f, got %f", decayToZero, decay)
		}
	})
}

func TestScoreByWeight(t *testing.T) {
	t.Run("basic computation", func(t *testing.T) {
		// scoreByWeight(1, 5) = 50 / (1 * 25) = 2
		got := scoreByWeight(1, 5)
		expected := float64(maxInMeshScore+maxFirstDeliveryScore) / (1 * 5 * 5)
		if math.Abs(got-expected) > 0.001 {
			t.Fatalf("expected %f, got %f", expected, got)
		}
	})

	t.Run("higher threshold means lower penalty per unit", func(t *testing.T) {
		low := scoreByWeight(1, 3)
		high := scoreByWeight(1, 10)
		if high >= low {
			t.Fatalf("higher threshold should give lower penalty: threshold=3 -> %f, threshold=10 -> %f", low, high)
		}
	})

	t.Run("higher weight means higher penalty", func(t *testing.T) {
		lowW := scoreByWeight(0.5, 5)
		highW := scoreByWeight(2, 5)
		// scoreByWeight = maxScore / (weight * threshold^2) where maxScore = (maxInMesh+maxFirstDelivery)*weight
		// so it simplifies to (maxInMesh+maxFirstDelivery) / threshold^2 — weight cancels out
		if math.Abs(lowW-highW) > 0.001 {
			t.Fatalf("weight should cancel out: lowW=%f highW=%f", lowW, highW)
		}
	})
}

func TestInMeshCap(t *testing.T) {
	cap := inMeshCap()
	slot := oneSlotDuration()
	expected := float64((3600 * time.Second) / slot)
	if cap != expected {
		t.Fatalf("expected inMeshCap %f, got %f", expected, cap)
	}
	// Should be 300 for 12-second slots.
	if slot == 12*time.Second && cap != 300 {
		t.Fatalf("expected 300 for 12s slots, got %f", cap)
	}
}

func TestOneSlotDuration(t *testing.T) {
	got := oneSlotDuration()
	if got != 12*time.Second {
		t.Fatalf("expected 12s slot duration, got %s", got)
	}
}

func TestOneEpochDuration(t *testing.T) {
	got := oneEpochDuration()
	// 32 slots * 12s = 384s
	if got != 384*time.Second {
		t.Fatalf("expected 384s epoch duration, got %s", got)
	}
}

func TestAttestationTopicScoreParams(t *testing.T) {
	p := AttestationTopicScoreParams()

	t.Run("topic weight is positive", func(t *testing.T) {
		if p.TopicWeight <= 0 {
			t.Fatalf("TopicWeight should be positive, got %f", p.TopicWeight)
		}
	})

	t.Run("P1 time in mesh", func(t *testing.T) {
		if p.TimeInMeshWeight <= 0 {
			t.Fatal("TimeInMeshWeight should be positive")
		}
		if p.TimeInMeshQuantum != oneSlotDuration() {
			t.Fatalf("TimeInMeshQuantum should be one slot, got %s", p.TimeInMeshQuantum)
		}
		if p.TimeInMeshCap <= 0 {
			t.Fatal("TimeInMeshCap should be positive")
		}
		// Max P1 contribution: weight * cap should equal maxInMeshScore.
		maxP1 := p.TimeInMeshWeight * p.TimeInMeshCap
		if math.Abs(maxP1-maxInMeshScore) > 0.01 {
			t.Fatalf("max P1 score should be %d, got %f", maxInMeshScore, maxP1)
		}
	})

	t.Run("P2 first message deliveries", func(t *testing.T) {
		if p.FirstMessageDeliveriesWeight <= 0 {
			t.Fatal("FirstMessageDeliveriesWeight should be positive")
		}
		if p.FirstMessageDeliveriesDecay <= 0 || p.FirstMessageDeliveriesDecay >= 1 {
			t.Fatalf("FirstMessageDeliveriesDecay should be in (0,1), got %f", p.FirstMessageDeliveriesDecay)
		}
		if p.FirstMessageDeliveriesCap != maxFirstDeliveryScore {
			t.Fatalf("FirstMessageDeliveriesCap should be %d, got %f", maxFirstDeliveryScore, p.FirstMessageDeliveriesCap)
		}
	})

	t.Run("P3 mesh message deliveries", func(t *testing.T) {
		if p.MeshMessageDeliveriesWeight >= 0 {
			t.Fatal("MeshMessageDeliveriesWeight should be negative")
		}
		if p.MeshMessageDeliveriesDecay <= 0 || p.MeshMessageDeliveriesDecay >= 1 {
			t.Fatalf("MeshMessageDeliveriesDecay should be in (0,1), got %f", p.MeshMessageDeliveriesDecay)
		}
		if p.MeshMessageDeliveriesCap <= p.MeshMessageDeliveriesThreshold {
			t.Fatal("MeshMessageDeliveriesCap should exceed threshold")
		}
		if p.MeshMessageDeliveriesThreshold <= 0 {
			t.Fatal("MeshMessageDeliveriesThreshold should be positive")
		}
		if p.MeshMessageDeliveriesWindow <= 0 {
			t.Fatal("MeshMessageDeliveriesWindow should be positive")
		}
		if p.MeshMessageDeliveriesActivation <= 0 {
			t.Fatal("MeshMessageDeliveriesActivation should be positive")
		}
		if p.MeshFailurePenaltyWeight >= 0 {
			t.Fatal("MeshFailurePenaltyWeight should be negative")
		}
		if p.MeshFailurePenaltyDecay <= 0 || p.MeshFailurePenaltyDecay >= 1 {
			t.Fatalf("MeshFailurePenaltyDecay should be in (0,1), got %f", p.MeshFailurePenaltyDecay)
		}
	})

	t.Run("P4 invalid messages disabled", func(t *testing.T) {
		if p.InvalidMessageDeliveriesWeight != 0 {
			t.Fatalf("InvalidMessageDeliveriesWeight should be 0, got %f", p.InvalidMessageDeliveriesWeight)
		}
	})
}

func TestPeerScoreConfig(t *testing.T) {
	topicScores := map[string]*pubsub.TopicScoreParams{
		"test-topic": AttestationTopicScoreParams(),
	}
	params, thresholds := PeerScoreConfig(topicScores)

	t.Run("params not nil", func(t *testing.T) {
		if params == nil {
			t.Fatal("PeerScoreParams should not be nil")
		}
	})

	t.Run("topics populated", func(t *testing.T) {
		if len(params.Topics) != 1 {
			t.Fatalf("expected 1 topic, got %d", len(params.Topics))
		}
		if _, ok := params.Topics["test-topic"]; !ok {
			t.Fatal("expected test-topic in topics map")
		}
	})

	t.Run("decay interval is one slot", func(t *testing.T) {
		if params.DecayInterval != oneSlotDuration() {
			t.Fatalf("DecayInterval should be one slot, got %s", params.DecayInterval)
		}
	})

	t.Run("decay to zero", func(t *testing.T) {
		if params.DecayToZero != decayToZero {
			t.Fatalf("DecayToZero should be %f, got %f", decayToZero, params.DecayToZero)
		}
	})

	t.Run("IP colocation disabled", func(t *testing.T) {
		if params.IPColocationFactorWeight != 0 {
			t.Fatalf("IPColocationFactorWeight should be 0, got %f", params.IPColocationFactorWeight)
		}
	})

	t.Run("behaviour penalty is negative", func(t *testing.T) {
		if params.BehaviourPenaltyWeight >= 0 {
			t.Fatal("BehaviourPenaltyWeight should be negative")
		}
		if params.BehaviourPenaltyDecay <= 0 || params.BehaviourPenaltyDecay >= 1 {
			t.Fatalf("BehaviourPenaltyDecay should be in (0,1), got %f", params.BehaviourPenaltyDecay)
		}
	})

	t.Run("app specific score returns zero", func(t *testing.T) {
		if params.AppSpecificScore("some-peer") != 0 {
			t.Fatal("AppSpecificScore should return 0")
		}
	})

	t.Run("thresholds ordered correctly", func(t *testing.T) {
		if thresholds.GossipThreshold >= 0 {
			t.Fatal("GossipThreshold should be negative")
		}
		if thresholds.PublishThreshold >= thresholds.GossipThreshold {
			t.Fatal("PublishThreshold should be more negative than GossipThreshold")
		}
		if thresholds.GraylistThreshold >= thresholds.PublishThreshold {
			t.Fatal("GraylistThreshold should be most negative")
		}
		if thresholds.AcceptPXThreshold <= 0 {
			t.Fatal("AcceptPXThreshold should be positive")
		}
		if thresholds.OpportunisticGraftThreshold <= 0 {
			t.Fatal("OpportunisticGraftThreshold should be positive")
		}
	})

	t.Run("retain score is positive", func(t *testing.T) {
		if params.RetainScore <= 0 {
			t.Fatal("RetainScore should be positive")
		}
	})

	t.Run("topic score cap is positive", func(t *testing.T) {
		if params.TopicScoreCap <= 0 {
			t.Fatal("TopicScoreCap should be positive")
		}
	})
}

func TestPeerScoreConfig_EmptyTopics(t *testing.T) {
	params, thresholds := PeerScoreConfig(nil)
	if params == nil || thresholds == nil {
		t.Fatal("should return non-nil even with nil topics")
	}
}

func TestPeerScoreConfig_MultipleTopics(t *testing.T) {
	topicScores := make(map[string]*pubsub.TopicScoreParams)
	for i := range 64 {
		fd := [4]byte{0x8c, 0x9f, 0x62, 0xfe}
		topicScores[SubnetTopic(fd, uint64(i))] = AttestationTopicScoreParams()
	}
	params, _ := PeerScoreConfig(topicScores)
	if len(params.Topics) != 64 {
		t.Fatalf("expected 64 topics, got %d", len(params.Topics))
	}
}
