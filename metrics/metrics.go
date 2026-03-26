package metrics

import (
	"log/slog"
	"net/http"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promhttp"
)

var latencyBuckets = []float64{-1, -0.5, -0.1, 0, 0.1, 0.25, 0.5, 1, 2, 3, 4, 5, 6, 8, 10, 12}
var blockBuckets = []float64{0.1, 0.25, 0.5, 1, 2, 3, 4, 5, 6, 8, 10, 12}

// Metrics holds per-instance Prometheus metrics. Each instance gets its own
// Metrics with a "probe" label automatically prepended via WrapRegistererWith.
type Metrics struct {
	AttestationLatency               *prometheus.HistogramVec
	AttestationLatencyFromFourSeconds *prometheus.HistogramVec
	AttestationArrivalInSlot         *prometheus.HistogramVec
	AttestationsReceived             *prometheus.CounterVec
	LateAttestations                 *prometheus.CounterVec
	BlockArrivalInSlot               prometheus.Histogram
	BlocksReceived                   prometheus.Counter
	ConnectedPeers                   prometheus.Gauge
	QUICPeers                        prometheus.Gauge
	TCPPeers                         prometheus.Gauge
	PeerUserAgents                   *prometheus.GaugeVec
	DialAttempts                     prometheus.Counter
	DialBackoffs                     prometheus.Counter
}

// NewMetrics creates per-instance metrics registered under the given probe name.
func NewMetrics(probeName string, reg prometheus.Registerer) *Metrics {
	wrapped := prometheus.WrapRegistererWith(prometheus.Labels{"probe": probeName}, reg)

	m := &Metrics{
		AttestationLatency: prometheus.NewHistogramVec(
			prometheus.HistogramOpts{
				Name:    "beaconprobe_attestation_latency_seconds",
				Help:    "Attestation arrival delay relative to expected time (slot_start + 4s)",
				Buckets: latencyBuckets,
			},
			[]string{"subnet_id"},
		),
		AttestationLatencyFromFourSeconds: prometheus.NewHistogramVec(
			prometheus.HistogramOpts{
				Name:    "beaconprobe_attestation_latency_from_4s_seconds",
				Help:    "Attestation arrival delay relative to slot_start + 4s",
				Buckets: latencyBuckets,
			},
			[]string{"subnet_id"},
		),
		AttestationArrivalInSlot: prometheus.NewHistogramVec(
			prometheus.HistogramOpts{
				Name:    "beaconprobe_attestation_arrival_in_slot_seconds",
				Help:    "Time into the slot when attestation was received",
				Buckets: latencyBuckets,
			},
			[]string{"subnet_id"},
		),
		AttestationsReceived: prometheus.NewCounterVec(
			prometheus.CounterOpts{
				Name: "beaconprobe_attestations_received_total",
				Help: "Total attestations received",
			},
			[]string{"subnet_id"},
		),
		LateAttestations: prometheus.NewCounterVec(
			prometheus.CounterOpts{
				Name: "beaconprobe_late_attestations_total",
				Help: "Attestations arriving after 8s into the slot",
			},
			[]string{"subnet_id"},
		),
		BlockArrivalInSlot: prometheus.NewHistogram(
			prometheus.HistogramOpts{
				Name:    "beaconprobe_block_arrival_in_slot_seconds",
				Help:    "Time into the slot when block was received",
				Buckets: blockBuckets,
			},
		),
		BlocksReceived: prometheus.NewCounter(
			prometheus.CounterOpts{
				Name: "beaconprobe_blocks_received_total",
				Help: "Total blocks received",
			},
		),
		ConnectedPeers: prometheus.NewGauge(
			prometheus.GaugeOpts{
				Name: "beaconprobe_connected_peers",
				Help: "Number of connected libp2p peers",
			},
		),
		QUICPeers: prometheus.NewGauge(
			prometheus.GaugeOpts{
				Name: "beaconprobe_quic_peers",
				Help: "Number of peers connected via QUIC",
			},
		),
		TCPPeers: prometheus.NewGauge(
			prometheus.GaugeOpts{
				Name: "beaconprobe_tcp_peers",
				Help: "Number of peers connected via TCP",
			},
		),
		PeerUserAgents: prometheus.NewGaugeVec(
			prometheus.GaugeOpts{
				Name: "beaconprobe_peer_user_agents",
				Help: "Number of connected peers by user agent",
			},
			[]string{"user_agent"},
		),
		DialAttempts: prometheus.NewCounter(
			prometheus.CounterOpts{
				Name: "beaconprobe_dial_attempts_total",
				Help: "Total dial attempts by peer manager",
			},
		),
		DialBackoffs: prometheus.NewCounter(
			prometheus.CounterOpts{
				Name: "beaconprobe_dial_backoffs_total",
				Help: "Total dials skipped due to backoff",
			},
		),
	}

	wrapped.MustRegister(
		m.AttestationLatency,
		m.AttestationLatencyFromFourSeconds,
		m.AttestationArrivalInSlot,
		m.AttestationsReceived,
		m.LateAttestations,
		m.BlockArrivalInSlot,
		m.BlocksReceived,
		m.ConnectedPeers,
		m.QUICPeers,
		m.TCPPeers,
		m.PeerUserAgents,
		m.DialAttempts,
		m.DialBackoffs,
	)

	return m
}

// DiscoveryPeersFound is a global metric (not per-instance) since discovery
// is shared across instances.
var DiscoveryPeersFound = prometheus.NewCounter(
	prometheus.CounterOpts{
		Name: "beaconprobe_discovery_peers_found_total",
		Help: "Total peers found via discv5",
	},
)

// RegisterDiscoveryMetrics registers the global discovery metrics on the given registry.
func RegisterDiscoveryMetrics(reg prometheus.Registerer) {
	reg.MustRegister(DiscoveryPeersFound)
}

// Serve starts the Prometheus metrics HTTP server using the given registry.
func Serve(addr string, reg *prometheus.Registry) {
	mux := http.NewServeMux()
	mux.Handle("/metrics", promhttp.HandlerFor(reg, promhttp.HandlerOpts{}))
	slog.Info("starting metrics server", "addr", addr)
	if err := http.ListenAndServe(addr, mux); err != nil {
		slog.Error("metrics server failed", "error", err)
	}
}
