package node

import (
	"path/filepath"
	"testing"

	"github.com/libp2p/go-libp2p"
	"github.com/libp2p/go-libp2p/core/host"
	"github.com/libp2p/go-libp2p/core/network"
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

func TestSubnetTopic(t *testing.T) {
	fd := [4]byte{0xab, 0xcd, 0xef, 0x01}
	got := SubnetTopic(fd, 5)
	want := "/eth2/abcdef01/beacon_attestation_5/ssz_snappy"
	if got != want {
		t.Fatalf("got %q, want %q", got, want)
	}
}

func TestSubnetTopic_ZeroDigest(t *testing.T) {
	fd := [4]byte{0x00, 0x00, 0x00, 0x00}
	got := SubnetTopic(fd, 0)
	want := "/eth2/00000000/beacon_attestation_0/ssz_snappy"
	if got != want {
		t.Fatalf("got %q, want %q", got, want)
	}
}

func TestNewHost(t *testing.T) {
	h, privKey, err := NewHost(0, 0, "") // port 0 = random
	if err != nil {
		t.Fatal(err)
	}
	defer h.Close()

	if privKey == nil {
		t.Fatal("expected non-nil private key")
	}
	if h.ID() == "" {
		t.Fatal("expected non-empty peer ID")
	}
	if len(h.Addrs()) == 0 {
		t.Fatal("expected at least one listen address")
	}
}

func TestNewGossipSub(t *testing.T) {
	h := newTestHost(t)
	ctx := t.Context()

	genesisValRoot := make([]byte, 32)
	ps, err := NewGossipSub(ctx, h, genesisValRoot, 8, filepath.Join(t.TempDir(), "gossipsub.log"))
	if err != nil {
		t.Fatal(err)
	}
	if ps == nil {
		t.Fatal("expected non-nil pubsub")
	}
}

func TestNewGossipSub_HighD(t *testing.T) {
	h := newTestHost(t)
	ctx := t.Context()

	genesisValRoot := make([]byte, 32)
	ps, err := NewGossipSub(ctx, h, genesisValRoot, 10000, filepath.Join(t.TempDir(), "gossipsub.log"))
	if err != nil {
		t.Fatal(err)
	}
	if ps == nil {
		t.Fatal("expected non-nil pubsub")
	}
}

func TestSubscribeSubnets(t *testing.T) {
	h := newTestHost(t)
	ctx := t.Context()

	genesisValRoot := make([]byte, 32)
	ps, err := NewGossipSub(ctx, h, genesisValRoot, 8, filepath.Join(t.TempDir(), "gossipsub.log"))
	if err != nil {
		t.Fatal(err)
	}

	fd := [4]byte{0x01, 0x02, 0x03, 0x04}
	subnetIDs := []uint64{0, 1, 5}
	subs, err := SubscribeSubnets(ps, fd, subnetIDs)
	if err != nil {
		t.Fatal(err)
	}

	if len(subs) != len(subnetIDs) {
		t.Fatalf("expected %d subscriptions, got %d", len(subnetIDs), len(subs))
	}

	for i, s := range subs {
		if s.SubnetID != subnetIDs[i] {
			t.Errorf("sub[%d].SubnetID = %d, want %d", i, s.SubnetID, subnetIDs[i])
		}
		if s.Topic == nil {
			t.Errorf("sub[%d].Topic is nil", i)
		}
		if s.Sub == nil {
			t.Errorf("sub[%d].Sub is nil", i)
		}
	}
}

func TestSubscribeSubnets_Empty(t *testing.T) {
	h := newTestHost(t)
	ctx := t.Context()

	genesisValRoot := make([]byte, 32)
	ps, err := NewGossipSub(ctx, h, genesisValRoot, 8, filepath.Join(t.TempDir(), "gossipsub.log"))
	if err != nil {
		t.Fatal(err)
	}

	fd := [4]byte{0x01, 0x02, 0x03, 0x04}
	subs, err := SubscribeSubnets(ps, fd, nil)
	if err != nil {
		t.Fatal(err)
	}
	if len(subs) != 0 {
		t.Fatalf("expected 0 subscriptions, got %d", len(subs))
	}
}
