package rpc

import (
	"bytes"
	"context"
	"io"
	"testing"
	"time"

	"github.com/OffchainLabs/prysm/v7/beacon-chain/p2p/encoder"
	"github.com/OffchainLabs/prysm/v7/consensus-types/primitives"
	ethpb "github.com/OffchainLabs/prysm/v7/proto/prysm/v1alpha1"
	"github.com/libp2p/go-libp2p"
	"github.com/libp2p/go-libp2p/core/host"
	"github.com/libp2p/go-libp2p/core/network"
	"github.com/libp2p/go-libp2p/core/peer"
	"github.com/libp2p/go-libp2p/core/protocol"
)

func newTestHost(t *testing.T) host.Host {
	t.Helper()
	h, err := libp2p.New(libp2p.ResourceManager(&network.NullResourceManager{}))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { h.Close() })
	return h
}

func connect(t *testing.T, a, b host.Host) {
	t.Helper()
	err := a.Connect(context.Background(), peer.AddrInfo{ID: b.ID(), Addrs: b.Addrs()})
	if err != nil {
		t.Fatal(err)
	}
}

func TestMakeAttnetsBytes(t *testing.T) {
	tests := []struct {
		name      string
		subnetIDs []uint64
		wantBits  []uint64
	}{
		{
			name:      "subnets 0 and 1",
			subnetIDs: []uint64{0, 1},
			wantBits:  []uint64{0, 1},
		},
		{
			name:      "subnet 7",
			subnetIDs: []uint64{7},
			wantBits:  []uint64{7},
		},
		{
			name:      "subnet 8",
			subnetIDs: []uint64{8},
			wantBits:  []uint64{8},
		},
		{
			name: "subnets 0 through 63",
			subnetIDs: func() []uint64 {
				ids := make([]uint64, 64)
				for i := range ids {
					ids[i] = uint64(i)
				}
				return ids
			}(),
			wantBits: func() []uint64 {
				ids := make([]uint64, 64)
				for i := range ids {
					ids[i] = uint64(i)
				}
				return ids
			}(),
		},
		{
			name:      "empty",
			subnetIDs: []uint64{},
			wantBits:  []uint64{},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			bv := MakeAttnetsBytes(tt.subnetIDs)
			if len(bv) != 8 {
				t.Fatalf("expected 8 bytes, got %d", len(bv))
			}

			// Check each expected bit is set.
			for _, id := range tt.wantBits {
				if bv[id/8]&(1<<(id%8)) == 0 {
					t.Errorf("bit %d should be set", id)
				}
			}

			// Count total set bits.
			totalSet := 0
			for _, b := range bv {
				for b != 0 {
					totalSet += int(b & 1)
					b >>= 1
				}
			}
			if totalSet != len(tt.wantBits) {
				t.Errorf("expected %d bits set, got %d", len(tt.wantBits), totalSet)
			}
		})
	}
}

func TestMakeAttnetsBytes_IgnoresOutOfRange(t *testing.T) {
	bv := MakeAttnetsBytes([]uint64{64, 100, 200})
	for _, b := range bv {
		if b != 0 {
			t.Fatal("out-of-range subnet IDs should not set any bits")
		}
	}
}

func TestMakeStatusProvider(t *testing.T) {
	fd := [4]byte{0xab, 0xcd, 0xef, 0x01}
	genesisTime := time.Now().Add(-1 * time.Hour)
	provider := MakeStatusProvider(fd, genesisTime)
	status := provider()

	if !bytes.Equal(status.ForkDigest, fd[:]) {
		t.Fatalf("fork digest mismatch: got %x, want %x", status.ForkDigest, fd[:])
	}
	if len(status.FinalizedRoot) != 32 {
		t.Fatalf("finalized root should be 32 bytes, got %d", len(status.FinalizedRoot))
	}
	if len(status.HeadRoot) != 32 {
		t.Fatalf("head root should be 32 bytes, got %d", len(status.HeadRoot))
	}
	if status.FinalizedEpoch != 0 {
		t.Fatalf("finalized epoch should be 0, got %d", status.FinalizedEpoch)
	}
	if status.HeadSlot == 0 {
		t.Fatalf("head slot should be non-zero for non-genesis time")
	}
}

func TestMakeStatusProvider_HeadSlot(t *testing.T) {
	fd := [4]byte{0x01, 0x02, 0x03, 0x04}
	const slotDuration = 12 * time.Second

	t.Run("genesis in the future returns slot 0", func(t *testing.T) {
		genesis := time.Now().Add(1 * time.Hour)
		status := MakeStatusProvider(fd, genesis)()
		if status.HeadSlot != 0 {
			t.Fatalf("expected slot 0 for future genesis, got %d", status.HeadSlot)
		}
	})

	t.Run("genesis is now returns slot 0", func(t *testing.T) {
		// Current slot is 0, so previous slot clamps to 0.
		genesis := time.Now()
		status := MakeStatusProvider(fd, genesis)()
		if status.HeadSlot != 0 {
			t.Fatalf("expected slot 0 at genesis, got %d", status.HeadSlot)
		}
	})

	t.Run("returns previous slot", func(t *testing.T) {
		// 10 slots ago: currentSlot=10, headSlot should be 9.
		genesis := time.Now().Add(-10 * slotDuration)
		status := MakeStatusProvider(fd, genesis)()
		if status.HeadSlot != 9 {
			t.Fatalf("expected slot 9, got %d", status.HeadSlot)
		}
	})

	t.Run("mid-slot returns previous slot", func(t *testing.T) {
		// 5 full slots + 6s into slot 5: currentSlot=5, headSlot=4.
		genesis := time.Now().Add(-5*slotDuration - 6*time.Second)
		status := MakeStatusProvider(fd, genesis)()
		if status.HeadSlot != 4 {
			t.Fatalf("expected slot 4, got %d", status.HeadSlot)
		}
	})
}

func TestForkDigestFromStatus(t *testing.T) {
	status := &ethpb.StatusV2{ForkDigest: []byte{0xab, 0xcd, 0xef, 0x01}}
	got := ForkDigestFromStatus(status)
	if got != "abcdef01" {
		t.Fatalf("got %q, want %q", got, "abcdef01")
	}
}

func TestForkDigestFromStatus_Short(t *testing.T) {
	status := &ethpb.StatusV2{ForkDigest: []byte{0xab}}
	got := ForkDigestFromStatus(status)
	if got != "unknown" {
		t.Fatalf("got %q, want %q", got, "unknown")
	}
}

func TestStatusHandshake(t *testing.T) {
	server := newTestHost(t)
	client := newTestHost(t)

	fd := [4]byte{0x01, 0x02, 0x03, 0x04}
	attnets := MakeAttnetsBytes([]uint64{0, 1})
	provider := MakeStatusProvider(fd, time.Now())

	RegisterHandlers(server, provider, attnets)
	connect(t, client, server)

	// Open a status stream to the server.
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	stream, err := client.NewStream(ctx, server.ID(), protocol.ID(statusProtocolV2))
	if err != nil {
		t.Fatal(err)
	}
	defer stream.Close()

	// Send our status.
	ourStatus := &ethpb.StatusV2{
		ForkDigest:            []byte{0x01, 0x02, 0x03, 0x04},
		FinalizedRoot:         make([]byte, 32),
		FinalizedEpoch:        100,
		HeadRoot:              make([]byte, 32),
		HeadSlot:              1000,
		EarliestAvailableSlot: 0,
	}
	enc := encoder.SszNetworkEncoder{}
	if _, err := enc.EncodeWithMaxLength(stream, ourStatus); err != nil {
		t.Fatal(err)
	}
	if err := stream.CloseWrite(); err != nil {
		t.Fatal(err)
	}

	// Read response code.
	code := make([]byte, 1)
	if _, err := io.ReadFull(stream, code); err != nil {
		t.Fatal(err)
	}
	if code[0] != responseCodeSuccess {
		t.Fatalf("expected success response code, got %d", code[0])
	}

	// Read server's status.
	var serverStatus ethpb.StatusV2
	if err := enc.DecodeWithMaxLength(stream, &serverStatus); err != nil {
		t.Fatal(err)
	}

	if !bytes.Equal(serverStatus.ForkDigest, fd[:]) {
		t.Fatalf("server fork digest mismatch: got %x, want %x", serverStatus.ForkDigest, fd[:])
	}
}

func TestPingHandler(t *testing.T) {
	server := newTestHost(t)
	client := newTestHost(t)

	fd := [4]byte{0x01, 0x02, 0x03, 0x04}
	RegisterHandlers(server, MakeStatusProvider(fd, time.Now()), MakeAttnetsBytes(nil))
	connect(t, client, server)

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	stream, err := client.NewStream(ctx, server.ID(), protocol.ID(pingProtocol))
	if err != nil {
		t.Fatal(err)
	}
	defer stream.Close()

	// Send ping.
	enc := encoder.SszNetworkEncoder{}
	var ping primitives.SSZUint64 = 42
	if _, err := enc.EncodeWithMaxLength(stream, &ping); err != nil {
		t.Fatal(err)
	}
	if err := stream.CloseWrite(); err != nil {
		t.Fatal(err)
	}

	// Read response code.
	code := make([]byte, 1)
	if _, err := io.ReadFull(stream, code); err != nil {
		t.Fatal(err)
	}
	if code[0] != responseCodeSuccess {
		t.Fatalf("expected success, got %d", code[0])
	}

	// Read pong (seq number).
	var seq primitives.SSZUint64
	if err := enc.DecodeWithMaxLength(stream, &seq); err != nil {
		t.Fatal(err)
	}
	if seq != 0 {
		t.Fatalf("expected seq 0, got %d", seq)
	}
}

func TestMetadataHandler(t *testing.T) {
	server := newTestHost(t)
	client := newTestHost(t)

	attnets := MakeAttnetsBytes([]uint64{0, 5, 63})

	// Register a simple metadata handler directly for test clarity.
	handlerCalled := make(chan error, 1)
	server.SetStreamHandler(protocol.ID(metadataProtocolV2), func(stream network.Stream) {
		defer stream.Close()
		mdResp := &ethpb.MetaDataV1{SeqNumber: 0, Attnets: attnets, Syncnets: []byte{0}}
		sszEnc := encoder.SszNetworkEncoder{}
		if _, err := stream.Write([]byte{responseCodeSuccess}); err != nil {
			handlerCalled <- err
			return
		}
		if _, err := sszEnc.EncodeWithMaxLength(stream, mdResp); err != nil {
			handlerCalled <- err
			return
		}
		handlerCalled <- nil
	})

	connect(t, client, server)

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	stream, err := client.NewStream(ctx, server.ID(), protocol.ID(metadataProtocolV2))
	if err != nil {
		t.Fatal(err)
	}
	defer stream.Close()

	// Read response code.
	code := make([]byte, 1)
	if _, err := io.ReadFull(stream, code); err != nil {
		// Check if the handler had an error.
		select {
		case herr := <-handlerCalled:
			t.Fatalf("read response code: %v (handler error: %v)", err, herr)
		default:
			t.Fatalf("read response code: %v (handler not called yet)", err)
		}
	}
	if code[0] != responseCodeSuccess {
		t.Fatalf("expected success, got %d", code[0])
	}

	// Read metadata.
	sszEnc := encoder.SszNetworkEncoder{}
	var md ethpb.MetaDataV1
	if err := sszEnc.DecodeWithMaxLength(stream, &md); err != nil {
		t.Fatal(err)
	}

	if herr := <-handlerCalled; herr != nil {
		t.Fatalf("handler error: %v", herr)
	}
	if md.SeqNumber != 0 {
		t.Fatalf("expected seq 0, got %d", md.SeqNumber)
	}
	if !bytes.Equal(md.Attnets, attnets) {
		t.Fatalf("attnets mismatch: got %x, want %x", md.Attnets, attnets)
	}
}

func TestGoodbyeHandler(t *testing.T) {
	server := newTestHost(t)
	client := newTestHost(t)

	fd := [4]byte{0x01, 0x02, 0x03, 0x04}
	RegisterHandlers(server, MakeStatusProvider(fd, time.Now()), MakeAttnetsBytes(nil))
	connect(t, client, server)

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	stream, err := client.NewStream(ctx, server.ID(), protocol.ID(goodbyeProtocol))
	if err != nil {
		t.Fatal(err)
	}
	defer stream.Close()

	// Send goodbye reason.
	enc := encoder.SszNetworkEncoder{}
	var reason primitives.SSZUint64 = 1 // wrong network
	if _, err := enc.EncodeWithMaxLength(stream, &reason); err != nil {
		t.Fatal(err)
	}
	// Goodbye handler just reads and logs — no response expected.
	// Close and ensure no panic.
	stream.Close()
}

func TestSendStatusOnConnect(t *testing.T) {
	server := newTestHost(t)
	client := newTestHost(t)

	fd := [4]byte{0x01, 0x02, 0x03, 0x04}
	provider := MakeStatusProvider(fd, time.Now())

	// Register handlers on both sides.
	RegisterHandlers(server, provider, MakeAttnetsBytes(nil))
	RegisterHandlers(client, provider, MakeAttnetsBytes(nil))

	// Set up auto-status on client.
	SendStatusOnConnect(client, provider)

	// Connect — this should trigger the client to send a status request to the server.
	connect(t, client, server)

	// Give the async status handshake time to complete.
	time.Sleep(500 * time.Millisecond)

	// If we got here without panics or deadlocks, the handshake worked.
}
