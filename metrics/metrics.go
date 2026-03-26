package metrics

import (
	"log/slog"
	"net/http"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promhttp"
)

var latencyBuckets = []float64{-1, -0.5, -0.1, 0, 0.1, 0.25, 0.5, 1, 2, 3, 4, 5, 6, 8, 10, 12}
var blockBuckets = []float64{0.1, 0.25, 0.5, 1, 2, 3, 4, 5, 6, 8, 10, 12}

// Global collectors registered via init(). Each has "probe" as a label dimension.
var (
	attestationLatency = prometheus.NewHistogramVec(
		prometheus.HistogramOpts{
			Name:    "beaconprobe_attestation_latency_seconds",
			Help:    "Attestation arrival delay relative to expected time (slot_start + 4s)",
			Buckets: latencyBuckets,
		},
		[]string{"probe", "subnet_id"},
	)
	attestationLatencyFromFourSeconds = prometheus.NewHistogramVec(
		prometheus.HistogramOpts{
			Name:    "beaconprobe_attestation_latency_from_4s_seconds",
			Help:    "Attestation arrival delay relative to slot_start + 4s",
			Buckets: latencyBuckets,
		},
		[]string{"probe", "subnet_id"},
	)
	attestationArrivalInSlot = prometheus.NewHistogramVec(
		prometheus.HistogramOpts{
			Name:    "beaconprobe_attestation_arrival_in_slot_seconds",
			Help:    "Time into the slot when attestation was received",
			Buckets: latencyBuckets,
		},
		[]string{"probe", "subnet_id"},
	)
	attestationsReceived = prometheus.NewCounterVec(
		prometheus.CounterOpts{
			Name: "beaconprobe_attestations_received_total",
			Help: "Total attestations received",
		},
		[]string{"probe", "subnet_id"},
	)
	lateAttestations = prometheus.NewCounterVec(
		prometheus.CounterOpts{
			Name: "beaconprobe_late_attestations_total",
			Help: "Attestations arriving after 8s into the slot",
		},
		[]string{"probe", "subnet_id"},
	)
	blockArrivalInSlot = prometheus.NewHistogramVec(
		prometheus.HistogramOpts{
			Name:    "beaconprobe_block_arrival_in_slot_seconds",
			Help:    "Time into the slot when block was received",
			Buckets: blockBuckets,
		},
		[]string{"probe"},
	)
	blocksReceived = prometheus.NewCounterVec(
		prometheus.CounterOpts{
			Name: "beaconprobe_blocks_received_total",
			Help: "Total blocks received",
		},
		[]string{"probe"},
	)
	connectedPeers = prometheus.NewGaugeVec(
		prometheus.GaugeOpts{
			Name: "beaconprobe_connected_peers",
			Help: "Number of connected libp2p peers",
		},
		[]string{"probe"},
	)
	quicPeers = prometheus.NewGaugeVec(
		prometheus.GaugeOpts{
			Name: "beaconprobe_quic_peers",
			Help: "Number of peers connected via QUIC",
		},
		[]string{"probe"},
	)
	tcpPeers = prometheus.NewGaugeVec(
		prometheus.GaugeOpts{
			Name: "beaconprobe_tcp_peers",
			Help: "Number of peers connected via TCP",
		},
		[]string{"probe"},
	)
	peerUserAgents = prometheus.NewGaugeVec(
		prometheus.GaugeOpts{
			Name: "beaconprobe_peer_user_agents",
			Help: "Number of connected peers by user agent",
		},
		[]string{"probe", "user_agent"},
	)
	dialAttempts = prometheus.NewCounterVec(
		prometheus.CounterOpts{
			Name: "beaconprobe_dial_attempts_total",
			Help: "Total dial attempts by peer manager",
		},
		[]string{"probe"},
	)
	dialBackoffs = prometheus.NewCounterVec(
		prometheus.CounterOpts{
			Name: "beaconprobe_dial_backoffs_total",
			Help: "Total dials skipped due to backoff",
		},
		[]string{"probe"},
	)

	// DiscoveryPeersFound is a global metric (not per-instance) since discovery
	// is shared across instances.
	DiscoveryPeersFound = prometheus.NewCounter(
		prometheus.CounterOpts{
			Name: "beaconprobe_discovery_peers_found_total",
			Help: "Total peers found via discv5",
		},
	)
)

func init() {
	prometheus.MustRegister(
		attestationLatency,
		attestationLatencyFromFourSeconds,
		attestationArrivalInSlot,
		attestationsReceived,
		lateAttestations,
		blockArrivalInSlot,
		blocksReceived,
		connectedPeers,
		quicPeers,
		tcpPeers,
		peerUserAgents,
		dialAttempts,
		dialBackoffs,
		DiscoveryPeersFound,
	)
}

// Metrics holds per-instance curried metric views. The "probe" label is baked in
// so callers use the same API as before (e.g. m.AttestationsReceived.WithLabelValues(subnet)).
type Metrics struct {
	AttestationLatency               prometheus.ObserverVec
	AttestationLatencyFromFourSeconds prometheus.ObserverVec
	AttestationArrivalInSlot         prometheus.ObserverVec
	AttestationsReceived             *prometheus.CounterVec
	LateAttestations                 *prometheus.CounterVec
	BlockArrivalInSlot               prometheus.Observer
	BlocksReceived                   prometheus.Counter
	ConnectedPeers                   prometheus.Gauge
	QUICPeers                        prometheus.Gauge
	TCPPeers                         prometheus.Gauge
	PeerUserAgents                   *prometheus.GaugeVec
	DialAttempts                     prometheus.Counter
	DialBackoffs                     prometheus.Counter
}

// NewMetrics creates a per-instance view of the global metrics with the probe label curried in.
func NewMetrics(probeName string) *Metrics {
	probeLabel := prometheus.Labels{"probe": probeName}
	return &Metrics{
		AttestationLatency:               attestationLatency.MustCurryWith(probeLabel),
		AttestationLatencyFromFourSeconds: attestationLatencyFromFourSeconds.MustCurryWith(probeLabel),
		AttestationArrivalInSlot:         attestationArrivalInSlot.MustCurryWith(probeLabel),
		AttestationsReceived:             attestationsReceived.MustCurryWith(probeLabel),
		LateAttestations:                 lateAttestations.MustCurryWith(probeLabel),
		BlockArrivalInSlot:               blockArrivalInSlot.WithLabelValues(probeName),
		BlocksReceived:                   blocksReceived.WithLabelValues(probeName),
		ConnectedPeers:                   connectedPeers.WithLabelValues(probeName),
		QUICPeers:                        quicPeers.WithLabelValues(probeName),
		TCPPeers:                         tcpPeers.WithLabelValues(probeName),
		PeerUserAgents:                   peerUserAgents.MustCurryWith(probeLabel),
		DialAttempts:                     dialAttempts.WithLabelValues(probeName),
		DialBackoffs:                     dialBackoffs.WithLabelValues(probeName),
	}
}

// Serve starts the Prometheus metrics HTTP server on the default registry.
func Serve(addr string) {
	mux := http.NewServeMux()
	mux.Handle("/metrics", promhttp.Handler())
	slog.Info("starting metrics server", "addr", addr)
	if err := http.ListenAndServe(addr, mux); err != nil {
		slog.Error("metrics server failed", "error", err)
	}
}
