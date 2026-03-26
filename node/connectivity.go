package node

import (
	"context"
	"log/slog"
	"slices"
	"strings"
	"time"

	"github.com/libp2p/go-libp2p/core/host"
	"github.com/libp2p/go-libp2p/core/network"

	"github.com/sukunrt/beaconprobe/metrics"
)

// ReportConnectivity periodically updates per-instance connectivity metrics.
func ReportConnectivity(ctx context.Context, h host.Host, m *metrics.Metrics) {
	ticker := time.NewTicker(30 * time.Second)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			quic, tcp := CountPeersByTransport(h)
			total := quic + tcp
			m.ConnectedPeers.Set(float64(total))
			m.QUICPeers.Set(float64(quic))
			m.TCPPeers.Set(float64(tcp))
			slog.Info("connected peers", "total", total, "quic", quic, "tcp", tcp)
		}
	}
}

// CountPeersByTransport returns the count of QUIC and TCP peers.
func CountPeersByTransport(h host.Host) (quic, tcp int) {
	for _, p := range h.Network().Peers() {
		conns := h.Network().ConnsToPeer(p)
		if len(conns) == 0 {
			continue // peer disconnected between Peers() and ConnsToPeer()
		}
		if slices.ContainsFunc(conns, isQUICConn) {
			quic++
		} else {
			tcp++
		}
	}
	return
}

func isQUICConn(c network.Conn) bool {
	s := c.RemoteMultiaddr().String()
	return strings.Contains(s, "/udp/")
}
