package metrics

import (
	"log/slog"
	"net/http"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promhttp"
)

var (
	AttestationLatency = prometheus.NewHistogramVec(
		prometheus.HistogramOpts{
			Name:    "beaconprobe_attestation_latency_seconds",
			Help:    "Attestation arrival delay relative to expected time (slot_start + 4s)",
			Buckets: []float64{0.1, 0.25, 0.5, 1, 2, 3, 4, 5, 6, 8, 10, 12},
		},
		[]string{"subnet_id"},
	)

	AttestationArrivalInSlot = prometheus.NewHistogramVec(
		prometheus.HistogramOpts{
			Name:    "beaconprobe_attestation_arrival_in_slot_seconds",
			Help:    "Time into the slot when attestation was received",
			Buckets: []float64{0.1, 0.25, 0.5, 1, 2, 3, 4, 5, 6, 8, 10, 12},
		},
		[]string{"subnet_id"},
	)

	AttestationsReceived = prometheus.NewCounterVec(
		prometheus.CounterOpts{
			Name: "beaconprobe_attestations_received_total",
			Help: "Total attestations received",
		},
		[]string{"subnet_id"},
	)

	LateAttestations = prometheus.NewCounterVec(
		prometheus.CounterOpts{
			Name: "beaconprobe_late_attestations_total",
			Help: "Attestations arriving after 8s into the slot",
		},
		[]string{"subnet_id"},
	)

	ConnectedPeers = prometheus.NewGauge(
		prometheus.GaugeOpts{
			Name: "beaconprobe_connected_peers",
			Help: "Number of connected libp2p peers",
		},
	)

	MeshPeers = prometheus.NewGaugeVec(
		prometheus.GaugeOpts{
			Name: "beaconprobe_mesh_peers",
			Help: "Number of mesh peers per subnet topic",
		},
		[]string{"subnet_id"},
	)

	DiscoveryPeersFound = prometheus.NewCounter(
		prometheus.CounterOpts{
			Name: "beaconprobe_discovery_peers_found_total",
			Help: "Total peers found via discv5",
		},
	)

	QUICPeers = prometheus.NewGauge(
		prometheus.GaugeOpts{
			Name: "beaconprobe_quic_peers",
			Help: "Number of peers connected via QUIC",
		},
	)

	TCPPeers = prometheus.NewGauge(
		prometheus.GaugeOpts{
			Name: "beaconprobe_tcp_peers",
			Help: "Number of peers connected via TCP",
		},
	)
)

func init() {
	prometheus.MustRegister(
		AttestationLatency,
		AttestationArrivalInSlot,
		AttestationsReceived,
		LateAttestations,
		ConnectedPeers,
		MeshPeers,
		DiscoveryPeersFound,
		QUICPeers,
		TCPPeers,
	)
}

func Serve(addr string) {
	mux := http.NewServeMux()
	mux.Handle("/metrics", promhttp.Handler())
	slog.Info("starting metrics server", "addr", addr)
	if err := http.ListenAndServe(addr, mux); err != nil {
		slog.Error("metrics server failed", "error", err)
	}
}
