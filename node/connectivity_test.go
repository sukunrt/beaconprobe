package node

import (
	"testing"

	"github.com/libp2p/go-libp2p"
	"github.com/libp2p/go-libp2p/core/host"
	"github.com/libp2p/go-libp2p/core/network"
	"github.com/libp2p/go-libp2p/core/peer"
)

func newTCPTestHost(t *testing.T) host.Host {
	t.Helper()
	h, err := libp2p.New(
		libp2p.ResourceManager(&network.NullResourceManager{}),
		libp2p.ListenAddrStrings("/ip4/127.0.0.1/tcp/0"),
	)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { h.Close() })
	return h
}

func TestCountPeersByTransport_NoPeers(t *testing.T) {
	h := newTestHost(t)
	quic, tcp := CountPeersByTransport(h)
	if quic != 0 || tcp != 0 {
		t.Fatalf("expected 0/0, got quic=%d tcp=%d", quic, tcp)
	}
}

func TestCountPeersByTransport_TCPPeers(t *testing.T) {
	h := newTCPTestHost(t)

	// Connect two TCP-only peers.
	for range 2 {
		p := newTCPTestHost(t)
		if err := h.Connect(t.Context(), peer.AddrInfo{ID: p.ID(), Addrs: p.Addrs()}); err != nil {
			t.Fatal(err)
		}
	}

	quic, tcp := CountPeersByTransport(h)
	if tcp != 2 {
		t.Fatalf("expected 2 TCP peers, got %d", tcp)
	}
	if quic != 0 {
		t.Fatalf("expected 0 QUIC peers, got %d", quic)
	}
}
