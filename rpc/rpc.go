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
	"github.com/libp2p/go-libp2p/core/host"
	"github.com/libp2p/go-libp2p/core/network"
	"github.com/libp2p/go-libp2p/core/peer"
	"github.com/libp2p/go-libp2p/core/protocol"
)

const (
	statusProtocol   = "/eth2/beacon_chain/req/status/1/ssz_snappy"
	pingProtocol     = "/eth2/beacon_chain/req/ping/1/ssz_snappy"
	metadataProtocol = "/eth2/beacon_chain/req/metadata/2/ssz_snappy"
	goodbyeProtocol  = "/eth2/beacon_chain/req/goodbye/1/ssz_snappy"

	responseCodeSuccess = byte(0x00)

	streamTimeout = 10 * time.Second
)

var enc = encoder.SszNetworkEncoder{}

// StatusProvider returns our current Status message.
type StatusProvider func() *ethpb.Status

// RegisterHandlers sets up RPC stream handlers on the host.
func RegisterHandlers(h host.Host, statusProvider StatusProvider, attnets []byte) {
	h.SetStreamHandler(protocol.ID(statusProtocol), func(stream network.Stream) {
		defer stream.Close()
		handleStatus(stream, statusProvider)
	})

	h.SetStreamHandler(protocol.ID(pingProtocol), func(stream network.Stream) {
		defer stream.Close()
		handlePing(stream)
	})

	h.SetStreamHandler(protocol.ID(metadataProtocol), func(stream network.Stream) {
		defer stream.Close()
		handleMetadata(stream, attnets)
	})

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

	// Read the peer's status.
	var peerStatus ethpb.Status
	if err := enc.DecodeWithMaxLength(stream, &peerStatus); err != nil {
		slog.Debug("failed to decode peer status", "error", err)
		return
	}

	slog.Debug("received status request",
		"peer", peerShort(stream.Conn().RemotePeer()),
		"headSlot", peerStatus.HeadSlot,
	)

	// Respond with our status.
	ourStatus := statusProvider()
	if _, err := stream.Write([]byte{responseCodeSuccess}); err != nil {
		slog.Debug("failed to write status response code", "error", err)
		return
	}
	if _, err := enc.EncodeWithMaxLength(stream, ourStatus); err != nil {
		slog.Debug("failed to encode status response", "error", err)
		return
	}
}

func handlePing(stream network.Stream) {
	stream.SetDeadline(time.Now().Add(streamTimeout))

	var ping primitives.SSZUint64
	if err := enc.DecodeWithMaxLength(stream, &ping); err != nil {
		slog.Debug("failed to decode ping", "error", err)
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

	// MetaData request has no body. Respond with our metadata.
	md := &ethpb.MetaDataV1{
		SeqNumber: 0,
		Attnets:   attnets,
		Syncnets:  []byte{0},
	}

	if _, err := stream.Write([]byte{responseCodeSuccess}); err != nil {
		return
	}
	if _, err := enc.EncodeWithMaxLength(stream, md); err != nil {
		slog.Debug("failed to encode metadata response", "error", err)
		return
	}
}

func handleGoodbye(stream network.Stream) {
	stream.SetDeadline(time.Now().Add(streamTimeout))

	var reason primitives.SSZUint64
	if err := enc.DecodeWithMaxLength(stream, &reason); err != nil {
		slog.Debug("failed to decode goodbye", "error", err)
		return
	}

	slog.Debug("received goodbye",
		"peer", peerShort(stream.Conn().RemotePeer()),
		"reason", uint64(reason),
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

	stream, err := h.NewStream(ctx, peerID, protocol.ID(statusProtocol))
	if err != nil {
		slog.Debug("failed to open status stream", "peer", peerShort(peerID), "error", err)
		return
	}
	defer stream.Close()
	stream.SetDeadline(time.Now().Add(streamTimeout))

	// Send our status.
	ourStatus := statusProvider()
	if _, err := enc.EncodeWithMaxLength(stream, ourStatus); err != nil {
		slog.Debug("failed to encode outbound status", "error", err)
		return
	}
	if err := stream.CloseWrite(); err != nil {
		slog.Debug("failed to close write on status stream", "error", err)
		return
	}

	// Read the response code.
	code := make([]byte, 1)
	if _, err := io.ReadFull(stream, code); err != nil {
		slog.Debug("failed to read status response code", "error", err)
		return
	}
	if code[0] != responseCodeSuccess {
		slog.Debug("peer returned non-success status", "code", code[0])
		return
	}

	// Read peer's status response.
	var peerStatus ethpb.Status
	if err := enc.DecodeWithMaxLength(stream, &peerStatus); err != nil {
		slog.Debug("failed to decode peer status response", "error", err)
		return
	}

	slog.Debug("status handshake complete",
		"peer", peerShort(peerID),
		"headSlot", peerStatus.HeadSlot,
	)
}

// MakeStatusProvider returns a function that provides our Status message.
func MakeStatusProvider(forkDigest [4]byte) StatusProvider {
	return func() *ethpb.Status {
		return &ethpb.Status{
			ForkDigest:     forkDigest[:],
			FinalizedRoot:  make([]byte, 32),
			FinalizedEpoch: 0,
			HeadRoot:       make([]byte, 32),
			HeadSlot:       0,
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
func ForkDigestFromStatus(status *ethpb.Status) string {
	if len(status.ForkDigest) < 4 {
		return "unknown"
	}
	return hex.EncodeToString(status.ForkDigest[:4])
}
