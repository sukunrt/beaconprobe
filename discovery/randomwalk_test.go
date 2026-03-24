package discovery

import (
	"context"
	"crypto/ecdsa"
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"net"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/OffchainLabs/prysm/v7/config/params"
	ethpb "github.com/OffchainLabs/prysm/v7/proto/prysm/v1alpha1"
	"github.com/OffchainLabs/prysm/v7/time/slots"
	gcrypto "github.com/ethereum/go-ethereum/crypto"
	"github.com/ethereum/go-ethereum/p2p/discover"
	"github.com/ethereum/go-ethereum/p2p/enode"
	"github.com/ethereum/go-ethereum/p2p/enr"
	"github.com/libp2p/go-libp2p"
	"github.com/libp2p/go-libp2p/core/network"
	libp2pcrypto "github.com/libp2p/go-libp2p/core/crypto"
	"github.com/libp2p/go-libp2p/p2p/security/noise"
	"github.com/libp2p/go-libp2p/p2p/transport/tcp"
	libp2pquic "github.com/libp2p/go-libp2p/p2p/transport/quicreuse"
	quic "github.com/libp2p/go-libp2p/p2p/transport/quic"
	yamux "github.com/libp2p/go-libp2p/p2p/muxer/yamux"
)

func setupDiscv5(t *testing.T, port int) (*discover.UDPv5, [4]byte) {
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

	localNode := enode.NewLocalNode(db, privKey)
	localNode.SetFallbackIP(net.IPv4zero)
	localNode.SetFallbackUDP(port)

	const mainnetGenesisUnix = 1606824023
	genesisTime := time.Unix(mainnetGenesisUnix, 0)
	currentSlot := slots.CurrentSlot(genesisTime)
	currentEpoch := slots.ToEpoch(currentSlot)
	forkDigest := params.ForkDigest(currentEpoch)

	t.Logf("fork digest: %s", hex.EncodeToString(forkDigest[:]))

	enrForkID := &ethpb.ENRForkID{
		CurrentForkDigest: forkDigest[:],
		NextForkVersion:   forkDigest[:],
		NextForkEpoch:     params.BeaconConfig().FarFutureEpoch,
	}
	enrForkIDBytes, err := enrForkID.MarshalSSZ()
	if err != nil {
		t.Fatal(err)
	}
	localNode.Set(enr.WithEntry(eth2EnrKey, enrForkIDBytes))

	allSubnets := make([]byte, 8)
	for i := range allSubnets {
		allSubnets[i] = 0xFF
	}
	localNode.Set(enr.WithEntry(params.BeaconNetworkConfig().AttSubnetKey, allSubnets))

	bootAddrs := params.BeaconNetworkConfig().BootstrapNodes
	bootNodes := make([]*enode.Node, 0, len(bootAddrs))
	for _, addr := range bootAddrs {
		node, err := enode.Parse(enode.ValidSchemes, addr)
		if err != nil {
			continue
		}
		bootNodes = append(bootNodes, node)
	}
	t.Logf("bootnodes: %d", len(bootNodes))

	conn, err := net.ListenUDP("udp", &net.UDPAddr{IP: net.IPv4zero, Port: port})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { conn.Close() })

	listener, err := discover.ListenV5(conn, localNode, discover.Config{
		PrivateKey: privKey,
		Bootnodes:  bootNodes,
	})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { listener.Close() })

	return listener, forkDigest
}

// TestRandomWalkSpeed measures how fast discv5 RandomNodes returns peers,
// with and without fork digest filtering. Run manually:
//
//	go test -run TestRandomWalkSpeed -v -count=1 -timeout 180s ./discovery/
func TestRandomWalkSpeed(t *testing.T) {
	t.Skip("skipping live network test; run manually with -run TestRandomWalkSpeed")

	listener, forkDigest := setupDiscv5(t, 19022)
	duration := 60 * time.Second

	// Test 1: Raw random walk — count all nodes.
	t.Run("raw", func(t *testing.T) {
		iter := listener.RandomNodes()
		defer iter.Close()

		var total, matched int
		start := time.Now()
		deadline := start.Add(duration)

		for time.Now().Before(deadline) && iter.Next() {
			total++
			if matchesForkDigest(iter.Node(), forkDigest) {
				matched++
			}
			if total%100 == 0 {
				elapsed := time.Since(start)
				t.Logf("  %s: total=%d matched=%d (%.1f%%) rate=%.1f/s",
					elapsed.Round(time.Second), total, matched,
					float64(matched)/float64(total)*100,
					float64(total)/elapsed.Seconds())
			}
		}

		elapsed := time.Since(start)
		t.Logf("RESULT raw: total=%d matched=%d (%.1f%%) in %s — %.1f nodes/s, %.1f matched/s",
			total, matched,
			float64(matched)/float64(total)*100,
			elapsed.Round(time.Second),
			float64(total)/elapsed.Seconds(),
			float64(matched)/elapsed.Seconds())
	})

	// Test 2: Filtered walk — only fork-digest-matching nodes.
	t.Run("filtered", func(t *testing.T) {
		iter := enode.Filter(listener.RandomNodes(), func(n *enode.Node) bool {
			return matchesForkDigest(n, forkDigest)
		})
		defer iter.Close()

		var count int
		start := time.Now()
		deadline := start.Add(duration)

		for time.Now().Before(deadline) && iter.Next() {
			count++
			if count%100 == 0 {
				elapsed := time.Since(start)
				t.Logf("  %s: matched=%d rate=%.1f/s",
					elapsed.Round(time.Second), count,
					float64(count)/elapsed.Seconds())
			}
		}

		elapsed := time.Since(start)
		t.Logf("RESULT filtered: matched=%d in %s — %.1f matched/s",
			count, elapsed.Round(time.Second),
			float64(count)/elapsed.Seconds())
	})

	fmt.Println("done")
}

// TestRandomWalkWithDial measures discovery + actual libp2p connection rate.
// Run manually:
//
//	go test -run TestRandomWalkWithDial -v -count=1 -timeout 360s ./discovery/
func TestRandomWalkWithDial(t *testing.T) {
	t.Skip("skipping live network test; run manually with -run TestRandomWalkWithDial")

	listener, forkDigest := setupDiscv5(t, 19032)

	// Create a libp2p host for dialing.
	privKey, err := ecdsa.GenerateKey(gcrypto.S256(), rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	libp2pKey, err := libp2pcrypto.UnmarshalSecp256k1PrivateKey(gcrypto.FromECDSA(privKey))
	if err != nil {
		t.Fatal(err)
	}

	h, err := libp2p.New(
		libp2p.Identity(libp2pKey),
		libp2p.ListenAddrStrings(
			"/ip4/0.0.0.0/tcp/19033",
			"/ip4/0.0.0.0/udp/19034/quic-v1",
		),
		libp2p.Transport(tcp.NewTCPTransport),
		libp2p.Transport(quic.NewTransport),
		libp2p.QUICReuse(libp2pquic.NewConnManager),
		libp2p.Security(noise.ID, noise.New),
		libp2p.Muxer(yamux.ID, yamux.DefaultTransport),
		libp2p.DisableRelay(),
		libp2p.ResourceManager(&network.NullResourceManager{}),
	)
	if err != nil {
		t.Fatal(err)
	}
	defer h.Close()
	t.Logf("libp2p host: %s", h.ID())

	duration := 1 * time.Minute
	ctx, cancel := context.WithTimeout(context.Background(), duration)
	defer cancel()

	iter := enode.Filter(listener.RandomNodes(), func(n *enode.Node) bool {
		return matchesForkDigest(n, forkDigest)
	})
	defer iter.Close()

	sem := make(chan struct{}, 500)
	var (
		discovered atomic.Int64
		dialSucc   atomic.Int64
		dialFail   atomic.Int64
		mu         sync.Mutex
		seen       = make(map[enode.ID]struct{})
	)

	// Periodic stats logger.
	start := time.Now()
	go func() {
		ticker := time.NewTicker(15 * time.Second)
		defer ticker.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
				elapsed := time.Since(start).Seconds()
				d := discovered.Load()
				s := dialSucc.Load()
				f := dialFail.Load()
				t.Logf("  %s: discovered=%d dialed_ok=%d dialed_fail=%d | rates: disc=%.1f/s conn=%.1f/s",
					time.Since(start).Round(time.Second), d, s, f,
					float64(d)/elapsed, float64(s)/elapsed)
			}
		}
	}()

loop:
	for iter.Next() {
		if ctx.Err() != nil {
			break
		}

		node := iter.Node()

		// Dedup.
		mu.Lock()
		if _, ok := seen[node.ID()]; ok {
			mu.Unlock()
			continue
		}
		seen[node.ID()] = struct{}{}
		mu.Unlock()

		discovered.Add(1)

		addrInfo, err := enodeToAddrInfo(node)
		if err != nil || addrInfo == nil {
			continue
		}

		select {
		case sem <- struct{}{}:
		case <-ctx.Done():
			break loop
		}

		go func() {
			defer func() { <-sem }()
			dialCtx, dialCancel := context.WithTimeout(ctx, 2*time.Second)
			defer dialCancel()
			dialCtx = network.WithDialPeerTimeout(dialCtx, 2*time.Second)
			if err := h.Connect(dialCtx, *addrInfo); err != nil {
				dialFail.Add(1)
			} else {
				dialSucc.Add(1)
			}
		}()
	}

	// Wait for in-flight dials.
	for range cap(sem) {
		sem <- struct{}{}
	}

	elapsed := time.Since(start)
	d := discovered.Load()
	s := dialSucc.Load()
	f := dialFail.Load()
	t.Logf("RESULT: duration=%s discovered=%d connected=%d failed=%d",
		elapsed.Round(time.Second), d, s, f)
	t.Logf("  rates: discovery=%.1f/s connections=%.1f/s",
		float64(d)/elapsed.Seconds(), float64(s)/elapsed.Seconds())
	t.Logf("  success rate: %.1f%%", float64(s)/float64(s+f)*100)
	t.Logf("  connected peers at end: %d", len(h.Network().Peers()))
}
