package node

import (
	"context"
	"log/slog"

	"github.com/libp2p/go-libp2p/core/event"
	"github.com/libp2p/go-libp2p/core/host"
	"github.com/libp2p/go-libp2p/core/network"
)

// HandleInboundPeers subscribes to peer connectivity events and enforces
// disjoint peer sets across instances. If a newly connected peer is owned
// by a different instance, it is disconnected. Unassigned peers are claimed.
//
// This is a defense-in-depth safety net alongside SiblingGater.InterceptSecured,
// which already rejects inbound connections from peers owned by other instances.
// HandleInboundPeers catches any races where a peer passes the gater but gets
// assigned to another instance between InterceptSecured and connection establishment.
func HandleInboundPeers(ctx context.Context, h host.Host, instanceName string, peerSet *SharedPeerSet) {
	sub, err := h.EventBus().Subscribe(new(event.EvtPeerConnectednessChanged))
	if err != nil {
		slog.Error("failed to subscribe to connectedness events", "error", err)
		return
	}
	defer sub.Close()

	for {
		select {
		case <-ctx.Done():
			return
		case ev, ok := <-sub.Out():
			if !ok {
				return
			}
			e := ev.(event.EvtPeerConnectednessChanged)
			if e.Connectedness != network.Connected {
				continue
			}

			owner, _ := peerSet.Claim(e.Peer, instanceName)
			if owner != instanceName {
				slog.Info("inbound: closing peer owned by other instance",
					"peer", peerShort(e.Peer),
					"owner", owner,
					"instance", instanceName,
				)
				h.Network().ClosePeer(e.Peer)
			}
		}
	}
}
