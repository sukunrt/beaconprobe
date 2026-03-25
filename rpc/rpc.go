package rpc

import (
	"context"
	"encoding/hex"
	"io"
	"log/slog"
	"time"

	"github.com/OffchainLabs/prysm/v7/beacon-chain/p2p/encoder"
	"github.com/OffchainLabs/prysm/v7/consensus-types/primitives"
	ethpb "github.com/OffchainLabs/prysm/v7/proto/prysm/v1alpha1"
	fastssz "github.com/prysmaticlabs/fastssz"
	"github.com/libp2p/go-libp2p/core/host"
	"github.com/libp2p/go-libp2p/core/network"
	"github.com/libp2p/go-libp2p/core/peer"
	"github.com/libp2p/go-libp2p/core/protocol"
)

const (
	statusProtocolV1   = "/eth2/beacon_chain/req/status/1/ssz_snappy"
	statusProtocolV2   = "/eth2/beacon_chain/req/status/2/ssz_snappy"
	pingProtocol       = "/eth2/beacon_chain/req/ping/1/ssz_snappy"
	metadataProtocolV1 = "/eth2/beacon_chain/req/metadata/1/ssz_snappy"
	metadataProtocolV2 = "/eth2/beacon_chain/req/metadata/2/ssz_snappy"
	metadataProtocolV3 = "/eth2/beacon_chain/req/metadata/3/ssz_snappy"
	goodbyeProtocol    = "/eth2/beacon_chain/req/goodbye/1/ssz_snappy"

	responseCodeSuccess = byte(0x00)

	streamTimeout = 10 * time.Second
)

var enc = encoder.SszNetworkEncoder{}

// StatusProvider returns our current StatusV2 message.
type StatusProvider func() *ethpb.StatusV2

// RegisterHandlers sets up RPC stream handlers on the host.
func RegisterHandlers(h host.Host, statusProvider StatusProvider, attnets []byte) {
	statusHandler := func(stream network.Stream) {
		defer stream.Close()
		handleStatus(stream, statusProvider)
	}
	h.SetStreamHandler(protocol.ID(statusProtocolV1), statusHandler)
	h.SetStreamHandler(protocol.ID(statusProtocolV2), statusHandler)

	h.SetStreamHandler(protocol.ID(pingProtocol), func(stream network.Stream) {
		defer stream.Close()
		handlePing(stream)
	})

	metadataHandler := func(stream network.Stream) {
		defer stream.Close()
		handleMetadata(stream, attnets)
	}
	h.SetStreamHandler(protocol.ID(metadataProtocolV1), metadataHandler)
	h.SetStreamHandler(protocol.ID(metadataProtocolV2), metadataHandler)
	h.SetStreamHandler(protocol.ID(metadataProtocolV3), metadataHandler)

	h.SetStreamHandler(protocol.ID(goodbyeProtocol), func(stream network.Stream) {
		defer stream.Close()
		handleGoodbye(stream)
	})
}

func peerShort(id peer.ID) string {
	s := id.String()
	if len(s) > 16 {
		return s[:16]
	}
	return s
}

func handleStatus(stream network.Stream, statusProvider StatusProvider) {
	stream.SetDeadline(time.Now().Add(streamTimeout))

	// Read the peer's status. Try V2 first since it's a superset of V1.
	var peerStatus ethpb.StatusV2
	if err := enc.DecodeWithMaxLength(stream, &peerStatus); err != nil {
		slog.Info("failed to decode peer status", "error", err)
		return
	}

	slog.Info("received status request",
		"peer", peerShort(stream.Conn().RemotePeer()),
		"headSlot", peerStatus.HeadSlot,
	)

	// Respond with our status.
	ourStatus := statusProvider()
	if _, err := stream.Write([]byte{responseCodeSuccess}); err != nil {
		slog.Info("failed to write status response code", "error", err)
		return
	}
	if _, err := enc.EncodeWithMaxLength(stream, ourStatus); err != nil {
		slog.Info("failed to encode status response", "error", err)
		return
	}
}

func handlePing(stream network.Stream) {
	stream.SetDeadline(time.Now().Add(streamTimeout))

	var ping primitives.SSZUint64
	if err := enc.DecodeWithMaxLength(stream, &ping); err != nil {
		slog.Info("failed to decode ping", "error", err)
		return
	}

	// Respond with our metadata sequence number (0).
	var seq primitives.SSZUint64
	if _, err := stream.Write([]byte{responseCodeSuccess}); err != nil {
		return
	}
	if _, err := enc.EncodeWithMaxLength(stream, &seq); err != nil {
		return
	}
}

func handleMetadata(stream network.Stream, attnets []byte) {
	stream.SetDeadline(time.Now().Add(streamTimeout))

	if _, err := stream.Write([]byte{responseCodeSuccess}); err != nil {
		return
	}

	// Respond with the appropriate metadata version based on stream protocol.
	var md fastssz.Marshaler
	switch string(stream.Protocol()) {
	case metadataProtocolV3:
		md = &ethpb.MetaDataV2{
			SeqNumber:         0,
			Attnets:           attnets,
			Syncnets:          []byte{0},
			CustodyGroupCount: 4,
		}
	case metadataProtocolV1:
		md = &ethpb.MetaDataV0{
			SeqNumber: 0,
			Attnets:   attnets,
		}
	default: // V2
		md = &ethpb.MetaDataV1{
			SeqNumber: 0,
			Attnets:   attnets,
			Syncnets:  []byte{0},
		}
	}

	if _, err := enc.EncodeWithMaxLength(stream, md); err != nil {
		slog.Info("failed to encode metadata response", "error", err)
		return
	}
}

var goodbyeReasons = map[uint64]string{
	1:   "client shutdown",
	2:   "irrelevant network",
	3:   "fault/error",
	128: "unable to verify network",
	129: "too many peers",
	250: "peer score too low",
	251: "client banned this node",
}

func handleGoodbye(stream network.Stream) {
	stream.SetDeadline(time.Now().Add(streamTimeout))

	var reason primitives.SSZUint64
	if err := enc.DecodeWithMaxLength(stream, &reason); err != nil {
		slog.Info("failed to decode goodbye", "error", err)
		return
	}

	code := uint64(reason)
	msg := goodbyeReasons[code]
	if msg == "" {
		msg = "unknown"
	}

	slog.Info("received goodbye",
		"peer", peerShort(stream.Conn().RemotePeer()),
		"reason", msg,
		"code", code,
	)
}

// SendStatusOnConnect sends a Status request to every newly connected peer.
func SendStatusOnConnect(h host.Host, statusProvider StatusProvider) {
	notifier := &network.NotifyBundle{
		ConnectedF: func(_ network.Network, conn network.Conn) {
			go sendStatus(h, conn.RemotePeer(), statusProvider)
		},
	}
	h.Network().Notify(notifier)
}

func sendStatus(h host.Host, peerID peer.ID, statusProvider StatusProvider) {
	ctx, cancel := context.WithTimeout(context.Background(), streamTimeout)
	defer cancel()

	stream, err := h.NewStream(ctx, peerID, protocol.ID(statusProtocolV2), protocol.ID(statusProtocolV1))
	if err != nil {
		slog.Info("failed to open status stream", "peer", peerShort(peerID), "error", err)
		return
	}
	defer stream.Close()
	stream.SetDeadline(time.Now().Add(streamTimeout))

	// Send our status.
	ourStatus := statusProvider()
	if _, err := enc.EncodeWithMaxLength(stream, ourStatus); err != nil {
		slog.Info("failed to encode outbound status", "error", err)
		return
	}
	if err := stream.CloseWrite(); err != nil {
		slog.Info("failed to close write on status stream", "error", err)
		return
	}

	// Read the response code.
	code := make([]byte, 1)
	if _, err := io.ReadFull(stream, code); err != nil {
		slog.Info("failed to read status response code", "error", err)
		return
	}
	if code[0] != responseCodeSuccess {
		slog.Info("peer returned non-success status", "code", code[0])
		return
	}

	// Read peer's status response.
	var peerStatus ethpb.StatusV2
	if err := enc.DecodeWithMaxLength(stream, &peerStatus); err != nil {
		slog.Info("failed to decode peer status response", "error", err)
		return
	}

	slog.Info("status handshake complete",
		"peer", peerShort(peerID),
		"headSlot", peerStatus.HeadSlot,
	)
}

// MakeStatusProvider returns a function that provides our StatusV2 message.
func MakeStatusProvider(forkDigest [4]byte) StatusProvider {
	return func() *ethpb.StatusV2 {
		return &ethpb.StatusV2{
			ForkDigest:            forkDigest[:],
			FinalizedRoot:         make([]byte, 32),
			FinalizedEpoch:        0,
			HeadRoot:              make([]byte, 32),
			HeadSlot:              0,
			EarliestAvailableSlot: 0,
		}
	}
}

// MakeAttnetsBytes creates the attnets bitvector bytes for the given subnet IDs.
func MakeAttnetsBytes(subnetIDs []uint64) []byte {
	bv := make([]byte, 8) // Bitvector64 = 8 bytes
	for _, id := range subnetIDs {
		if id < 64 {
			bv[id/8] |= 1 << (id % 8)
		}
	}
	return bv
}

// ForkDigestFromStatus creates fork digest hex string from a status message.
func ForkDigestFromStatus(status *ethpb.StatusV2) string {
	if len(status.ForkDigest) < 4 {
		return "unknown"
	}
	return hex.EncodeToString(status.ForkDigest[:4])
}
