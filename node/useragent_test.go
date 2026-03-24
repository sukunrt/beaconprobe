package node

import (
	"testing"
	"time"

	"github.com/libp2p/go-libp2p"
	"github.com/libp2p/go-libp2p/core/network"
	"github.com/libp2p/go-libp2p/core/peer"
	"github.com/prometheus/client_golang/prometheus"
	dto "github.com/prometheus/client_model/go"
	"github.com/sukunrt/beaconprobe/metrics"
)

func TestNormalizeUserAgent(t *testing.T) {
	tests := []struct {
		input string
		want  string
	}{
		{"Prysm/v5.2.1/linux-amd64", "Prysm/v5"},
		{"Lighthouse/4.1.0", "Lighthouse/4"},
		{"Teku/v24.3.1", "Teku/v24"},
		{"nimbus", "nimbus"},
		{"", "unknown"},
		{"Prysm/v5.2.1/linux/amd64", "Prysm/v5"},
		{"Lodestar/v1.25.0/537dc08", "Lodestar/v1"},
		{"something/", "something/"},
	}
	for _, tt := range tests {
		t.Run(tt.input, func(t *testing.T) {
			got := normalizeUserAgent(tt.input)
			if got != tt.want {
				t.Errorf("normalizeUserAgent(%q) = %q, want %q", tt.input, got, tt.want)
			}
		})
	}
}

func gaugeValue(g *prometheus.GaugeVec, label string) float64 {
	var m dto.Metric
	if err := g.WithLabelValues(label).Write(&m); err != nil {
		return 0
	}
	return m.GetGauge().GetValue()
}

func TestTrackUserAgents(t *testing.T) {
	// Reset the gauge to avoid interference from other tests.
	metrics.PeerUserAgents.Reset()

	// Create two hosts. Host B has a known user agent.
	hostA, err := libp2p.New(
		libp2p.ResourceManager(&network.NullResourceManager{}),
		libp2p.ListenAddrStrings("/ip4/127.0.0.1/tcp/0"),
	)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { hostA.Close() })

	hostB, err := libp2p.New(
		libp2p.ResourceManager(&network.NullResourceManager{}),
		libp2p.ListenAddrStrings("/ip4/127.0.0.1/tcp/0"),
		libp2p.UserAgent("TestClient/v3.2.1/linux-amd64"),
	)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { hostB.Close() })

	ctx := t.Context()

	// Start tracking on host A.
	go TrackUserAgents(ctx, hostA)

	// Connect A → B.
	hostA.Peerstore().AddAddrs(hostB.ID(), hostB.Addrs(), time.Hour)
	if err := hostA.Connect(ctx, peer.AddrInfo{ID: hostB.ID(), Addrs: hostB.Addrs()}); err != nil {
		t.Fatal(err)
	}

	// Wait for identify to complete.
	time.Sleep(500 * time.Millisecond)

	if v := gaugeValue(metrics.PeerUserAgents, "TestClient/v3"); v != 1 {
		t.Errorf("expected gauge for TestClient/v3 = 1, got %v", v)
	}

	// Disconnect B.
	if err := hostA.Network().ClosePeer(hostB.ID()); err != nil {
		t.Fatal(err)
	}

	// Wait for disconnect event.
	time.Sleep(500 * time.Millisecond)

	if v := gaugeValue(metrics.PeerUserAgents, "TestClient/v3"); v != 0 {
		t.Errorf("expected gauge for TestClient/v3 = 0 after disconnect, got %v", v)
	}
}
