package node

import (
	"context"
	"log/slog"
	"strings"
	"time"

	"github.com/libp2p/go-libp2p/core/event"
	"github.com/libp2p/go-libp2p/core/host"
	"github.com/libp2p/go-libp2p/core/network"
	"github.com/libp2p/go-libp2p/core/peer"
	"github.com/sukunrt/beaconprobe/metrics"
)

func peerShort(id peer.ID) string {
	s := id.String()
	if len(s) > 16 {
		return s[:16]
	}
	return s
}

// normalizeUserAgent extracts "name/vMAJOR" from a full user agent string.
// For example, "Prysm/v5.2.1/linux-amd64" becomes "Prysm/v5".
func normalizeUserAgent(raw string) string {
	if raw == "" {
		return "unknown"
	}
	name, version, ok := strings.Cut(raw, "/")
	if !ok {
		return raw
	}
	// Strip the 'v' prefix to find the major version number, then re-add it.
	ver := version
	prefix := ""
	if strings.HasPrefix(ver, "v") {
		prefix = "v"
		ver = ver[1:]
	}
	major, _, _ := strings.Cut(ver, ".")
	return name + "/" + prefix + major
}

// TrackUserAgents subscribes to identify and disconnect events to maintain
// per-user-agent peer counts. It logs individual identify completions and
// periodically logs aggregate user agent counts.
func TrackUserAgents(ctx context.Context, h host.Host, m *metrics.Metrics) {
	identSub, err := h.EventBus().Subscribe(new(event.EvtPeerIdentificationCompleted))
	if err != nil {
		slog.Error("failed to subscribe to identify events", "error", err)
		return
	}
	defer identSub.Close()

	connSub, err := h.EventBus().Subscribe(new(event.EvtPeerConnectednessChanged))
	if err != nil {
		slog.Error("failed to subscribe to connectedness events", "error", err)
		return
	}
	defer connSub.Close()

	peers := make(map[peer.ID]string)    // peer → normalized user agent
	counts := make(map[string]int)        // normalized user agent → count

	ticker := time.NewTicker(30 * time.Second)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case ev, ok := <-identSub.Out():
			if !ok {
				return
			}
			ident := ev.(event.EvtPeerIdentificationCompleted)
			ua := normalizeUserAgent(ident.AgentVersion)

			// If we already tracked this peer (re-identify), remove old entry first.
			if old, exists := peers[ident.Peer]; exists {
				counts[old]--
				if counts[old] <= 0 {
					delete(counts, old)
				}
				m.PeerUserAgents.WithLabelValues(old).Dec()
			}

			peers[ident.Peer] = ua
			counts[ua]++
			m.PeerUserAgents.WithLabelValues(ua).Inc()
			slog.Info("identified peer", "peer", peerShort(ident.Peer), "user_agent", ident.AgentVersion, "normalized", ua)

		case ev, ok := <-connSub.Out():
			if !ok {
				return
			}
			conn := ev.(event.EvtPeerConnectednessChanged)
			if conn.Connectedness != network.NotConnected {
				continue
			}
			ua, exists := peers[conn.Peer]
			if !exists {
				continue
			}
			delete(peers, conn.Peer)
			counts[ua]--
			if counts[ua] <= 0 {
				delete(counts, ua)
			}
			m.PeerUserAgents.WithLabelValues(ua).Dec()

		case <-ticker.C:
			slog.Info("peer user agents", "counts", counts)
		}
	}
}
