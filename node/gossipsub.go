package node

import (
	"context"
	"encoding/hex"
	"fmt"
	"time"

	"github.com/OffchainLabs/prysm/v7/beacon-chain/p2p"
	"github.com/OffchainLabs/prysm/v7/config/params"
	pubsub "github.com/libp2p/go-libp2p-pubsub"
	pubsubpb "github.com/libp2p/go-libp2p-pubsub/pb"
	"github.com/libp2p/go-libp2p/core/host"
	"github.com/libp2p/go-libp2p/core/peer"
)

const gossipSubHeartbeatInterval = 700 * time.Millisecond

// NewGossipSub creates a gossipsub instance configured with eth2 parameters.
// d sets the target mesh degree; Dlo and Dhi are derived from it.
func NewGossipSub(ctx context.Context, h host.Host, genesisValRoot []byte, d int, disableIHave bool, forkDigest [4]byte, subnetIDs []uint64, gossipLogFile string) (*pubsub.PubSub, error) {
	gsParams := pubsub.DefaultGossipSubParams()
	gsParams.D = d
	gsParams.Dlo = max(d-2, 1)
	gsParams.Dhi = d + d/2
	gsParams.Dlazy = 6
	gsParams.HeartbeatInterval = gossipSubHeartbeatInterval
	gsParams.FanoutTTL = 60 * time.Second
	gsParams.HistoryLength = 6
	gsParams.HistoryGossip = 3

	// Pre-populate topic score params for all attestation subnets.
	topicScores := make(map[string]*pubsub.TopicScoreParams, len(subnetIDs))
	for _, id := range subnetIDs {
		topicScores[SubnetTopic(forkDigest, id)] = AttestationTopicScoreParams()
	}
	scoreParams, scoreThresholds := PeerScoreConfig(topicScores)

	opts := []pubsub.Option{
		pubsub.WithPeerScore(scoreParams, scoreThresholds),
		pubsub.WithGossipSubParams(gsParams),
		pubsub.WithMessageIdFn(func(pmsg *pubsubpb.Message) string {
			return p2p.MsgID(genesisValRoot, pmsg)
		}),
		pubsub.WithMessageSignaturePolicy(pubsub.StrictNoSign),
		pubsub.WithMaxMessageSize(int(params.BeaconConfig().MaxPayloadSize)),
		pubsub.WithValidateQueueSize(256),
		pubsub.WithPrometheusTracer(gossipLogFile),
	}
	if disableIHave {
		opts = append(opts, pubsub.WithDisableIHaveGossip(true))
	}
	return pubsub.NewGossipSub(ctx, h, opts...)
}

// SubnetTopic returns the gossipsub topic string for an attestation subnet.
func SubnetTopic(forkDigest [4]byte, subnetID uint64) string {
	return fmt.Sprintf("/eth2/%s/beacon_attestation_%d/ssz_snappy", hex.EncodeToString(forkDigest[:]), subnetID)
}

// Subscription holds a topic and its subscription for a subnet.
type Subscription struct {
	SubnetID uint64
	Topic    *pubsub.Topic
	Sub      *pubsub.Subscription
}

// SubscribeSubnets joins and subscribes to attestation subnet topics.
func SubscribeSubnets(ps *pubsub.PubSub, forkDigest [4]byte, subnetIDs []uint64) ([]Subscription, error) {
	subs := make([]Subscription, 0, len(subnetIDs))
	for _, id := range subnetIDs {
		topicStr := SubnetTopic(forkDigest, id)

		// Register a pass-through validator.
		if err := ps.RegisterTopicValidator(topicStr, func(_ context.Context, _ peer.ID, _ *pubsub.Message) pubsub.ValidationResult {
			return pubsub.ValidationAccept
		}); err != nil {
			return nil, fmt.Errorf("register validator for subnet %d: %w", id, err)
		}

		topic, err := ps.Join(topicStr)
		if err != nil {
			return nil, fmt.Errorf("join topic for subnet %d: %w", id, err)
		}

		sub, err := topic.Subscribe()
		if err != nil {
			return nil, fmt.Errorf("subscribe to subnet %d: %w", id, err)
		}

		subs = append(subs, Subscription{SubnetID: id, Topic: topic, Sub: sub})
	}
	return subs, nil
}
