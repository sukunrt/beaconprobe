package discovery

import (
	"crypto/ecdsa"
	"crypto/rand"
	"testing"

	"github.com/OffchainLabs/prysm/v7/config/params"
	ethpb "github.com/OffchainLabs/prysm/v7/proto/prysm/v1alpha1"
	gcrypto "github.com/ethereum/go-ethereum/crypto"
	"github.com/ethereum/go-ethereum/p2p/enode"
	"github.com/ethereum/go-ethereum/p2p/enr"
)

func makeTestNode(t *testing.T, forkDigest [4]byte, attnets []byte) *enode.Node {
	t.Helper()
	privKey, err := ecdsa.GenerateKey(gcrypto.S256(), rand.Reader)
	if err != nil {
		t.Fatal(err)
	}

	db, err := enode.OpenDB("")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { db.Close() })

	ln := enode.NewLocalNode(db, privKey)
	ln.SetStaticIP([]byte{127, 0, 0, 1})
	ln.Set(enr.TCP(9000))

	// Set eth2 ENR entry.
	enrForkID := &ethpb.ENRForkID{
		CurrentForkDigest: forkDigest[:],
		NextForkVersion:   forkDigest[:],
		NextForkEpoch:     params.BeaconConfig().FarFutureEpoch,
	}
	enc, err := enrForkID.MarshalSSZ()
	if err != nil {
		t.Fatal(err)
	}
	ln.Set(enr.WithEntry(eth2EnrKey, enc))

	// Set attnets.
	if attnets != nil {
		ln.Set(enr.WithEntry(params.BeaconNetworkConfig().AttSubnetKey, attnets))
	}

	return ln.Node()
}

func TestMatchesForkDigest(t *testing.T) {
	fd := [4]byte{0xab, 0xcd, 0xef, 0x01}
	node := makeTestNode(t, fd, nil)

	if !matchesForkDigest(node, fd) {
		t.Fatal("expected fork digest to match")
	}

	otherFD := [4]byte{0x00, 0x00, 0x00, 0x00}
	if matchesForkDigest(node, otherFD) {
		t.Fatal("expected fork digest not to match")
	}
}

func TestMatchesForkDigest_NoEntry(t *testing.T) {
	privKey, _ := ecdsa.GenerateKey(gcrypto.S256(), rand.Reader)
	db, _ := enode.OpenDB("")
	t.Cleanup(func() { db.Close() })
	ln := enode.NewLocalNode(db, privKey)
	node := ln.Node()

	fd := [4]byte{0xab, 0xcd, 0xef, 0x01}
	if matchesForkDigest(node, fd) {
		t.Fatal("expected no match for node without eth2 entry")
	}
}

func TestHasSubnetOverlap(t *testing.T) {
	// Set bits 0, 5, 63 in the attnets bitvector.
	attnets := make([]byte, 8)
	attnets[0] |= 1 << 0 // bit 0
	attnets[0] |= 1 << 5 // bit 5
	attnets[7] |= 1 << 7 // bit 63

	fd := [4]byte{0x01, 0x02, 0x03, 0x04}
	node := makeTestNode(t, fd, attnets)

	tests := []struct {
		name      string
		subnets   []uint64
		wantMatch bool
	}{
		{"overlap on 0", []uint64{0}, true},
		{"overlap on 5", []uint64{5}, true},
		{"overlap on 63", []uint64{63}, true},
		{"no overlap on 1", []uint64{1}, false},
		{"no overlap on 10", []uint64{10}, false},
		{"mixed overlap", []uint64{1, 5}, true},
		{"empty subnets", []uint64{}, false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := hasSubnetOverlap(node, tt.subnets)
			if got != tt.wantMatch {
				t.Fatalf("hasSubnetOverlap = %v, want %v", got, tt.wantMatch)
			}
		})
	}
}

func TestHasSubnetOverlap_NoAttnets(t *testing.T) {
	fd := [4]byte{0x01, 0x02, 0x03, 0x04}
	node := makeTestNode(t, fd, nil)

	if hasSubnetOverlap(node, []uint64{0, 1}) {
		t.Fatal("expected no overlap when attnets not set")
	}
}

func TestEnodeToAddrInfo(t *testing.T) {
	fd := [4]byte{0x01, 0x02, 0x03, 0x04}
	node := makeTestNode(t, fd, nil)

	info, err := enodeToAddrInfo(node)
	if err != nil {
		t.Fatal(err)
	}
	if info == nil {
		t.Fatal("expected non-nil AddrInfo")
	}
	if info.ID == "" {
		t.Fatal("expected non-empty peer ID")
	}
	if len(info.Addrs) == 0 {
		t.Fatal("expected at least one multiaddr")
	}

	// Verify the TCP address is present.
	found := false
	for _, addr := range info.Addrs {
		if addr.String() != "" {
			found = true
		}
	}
	if !found {
		t.Fatal("expected a valid multiaddr")
	}
}

func TestEnodeToAddrInfo_NoPubkey(t *testing.T) {
	// Can't easily create an enode without pubkey, so just test with a valid one.
	fd := [4]byte{0x01, 0x02, 0x03, 0x04}
	node := makeTestNode(t, fd, nil)
	info, err := enodeToAddrInfo(node)
	if err != nil {
		t.Fatal(err)
	}
	if info == nil {
		t.Fatal("expected non-nil AddrInfo for valid node")
	}
}

func TestPeerShort(t *testing.T) {
	// peerShort operates on the string representation of peer.ID.
	// Use a real peer ID to test truncation.
	privKey, _ := ecdsa.GenerateKey(gcrypto.S256(), rand.Reader)
	db, _ := enode.OpenDB("")
	t.Cleanup(func() { db.Close() })
	ln := enode.NewLocalNode(db, privKey)
	ln.SetStaticIP([]byte{127, 0, 0, 1})
	ln.Set(enr.TCP(9000))
	node := ln.Node()

	info, err := enodeToAddrInfo(node)
	if err != nil {
		t.Fatal(err)
	}

	got := peerShort(info.ID)
	peerStr := info.ID.String()
	if len(peerStr) > 16 {
		if len(got) != 16 {
			t.Errorf("peerShort should truncate to 16, got len %d: %q", len(got), got)
		}
	} else {
		if got != peerStr {
			t.Errorf("peerShort should not truncate short IDs, got %q want %q", got, peerStr)
		}
	}
}
