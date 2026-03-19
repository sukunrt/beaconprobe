package discovery

import (
	"context"
	"crypto/ecdsa"
	"fmt"
	"log/slog"
	"net"
	"time"

	"github.com/OffchainLabs/prysm/v7/config/params"
	ecdsaprysm "github.com/OffchainLabs/prysm/v7/crypto/ecdsa"
	ethpb "github.com/OffchainLabs/prysm/v7/proto/prysm/v1alpha1"
	"github.com/ethereum/go-ethereum/p2p/discover"
	"github.com/ethereum/go-ethereum/p2p/enode"
	"github.com/ethereum/go-ethereum/p2p/enr"
	"github.com/libp2p/go-libp2p/core/host"
	"github.com/libp2p/go-libp2p/core/peer"
	ma "github.com/multiformats/go-multiaddr"
	"github.com/sukunrt/beaconprobe/metrics"
)

const (
	eth2EnrKey = "eth2"
)

// Config holds discovery configuration.
type Config struct {
	PrivKey      *ecdsa.PrivateKey
	DiscPort     uint
	ForkDigest   [4]byte
	SubnetIDs    []uint64
	AttnetsBytes []byte
}

// StartDiscovery starts discv5 and a peer connection loop.
func StartDiscovery(ctx context.Context, h host.Host, cfg Config) error {
	// Create local ENR node.
	db, err := enode.OpenDB("")
	if err != nil {
		return fmt.Errorf("open enode db: %w", err)
	}
	localNode := enode.NewLocalNode(db, cfg.PrivKey)

	// Set the IP address.
	localNode.SetFallbackIP(net.IPv4zero)
	localNode.SetFallbackUDP(int(cfg.DiscPort))

	// Set eth2 ENR entry.
	enrForkID := &ethpb.ENRForkID{
		CurrentForkDigest: cfg.ForkDigest[:],
		NextForkVersion:   cfg.ForkDigest[:], // Same as current for simplicity.
		NextForkEpoch:     params.BeaconConfig().FarFutureEpoch,
	}
	enrForkIDBytes, err := enrForkID.MarshalSSZ()
	if err != nil {
		return fmt.Errorf("marshal ENR fork ID: %w", err)
	}
	localNode.Set(enr.WithEntry(eth2EnrKey, enrForkIDBytes))

	// Set attnets ENR entry.
	attSubnetKey := params.BeaconNetworkConfig().AttSubnetKey
	localNode.Set(enr.WithEntry(attSubnetKey, cfg.AttnetsBytes))

	// Parse bootnodes.
	bootAddrs := params.BeaconNetworkConfig().BootstrapNodes
	bootNodes := make([]*enode.Node, 0, len(bootAddrs))
	for _, addr := range bootAddrs {
		node, err := enode.Parse(enode.ValidSchemes, addr)
		if err != nil {
			slog.Warn("failed to parse bootnode", "addr", addr, "error", err)
			continue
		}
		bootNodes = append(bootNodes, node)
	}
	slog.Info("parsed bootnodes", "count", len(bootNodes))

	// Listen on UDP for discv5.
	udpAddr := &net.UDPAddr{IP: net.IPv4zero, Port: int(cfg.DiscPort)}
	conn, err := net.ListenUDP("udp", udpAddr)
	if err != nil {
		return fmt.Errorf("listen udp: %w", err)
	}

	// Start discv5 listener.
	dv5Cfg := discover.Config{
		PrivateKey: cfg.PrivKey,
		Bootnodes:  bootNodes,
	}
	listener, err := discover.ListenV5(conn, localNode, dv5Cfg)
	if err != nil {
		return fmt.Errorf("listen discv5: %w", err)
	}
	slog.Info("discv5 listener started")

	// Run peer discovery loop.
	go discoverPeers(ctx, h, listener, cfg)
	return nil
}

func discoverPeers(ctx context.Context, h host.Host, listener *discover.UDPv5, cfg Config) {
	iterator := listener.RandomNodes()
	defer iterator.Close()

	ticker := time.NewTicker(30 * time.Second)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			slog.Info("connected peers", "peers", len(h.Network().Peers()))
			metrics.ConnectedPeers.Set(float64(len(h.Network().Peers())))
		default:
		}

		if !iterator.Next() {
			return
		}
		node := iterator.Node()
		metrics.DiscoveryPeersFound.Inc()

		// Check eth2 ENR entry matches our fork digest.
		if !matchesForkDigest(node, cfg.ForkDigest) {
			continue
		}

		// Check attnets overlap with our target subnets.
		if !hasSubnetOverlap(node, cfg.SubnetIDs) {
			continue
		}

		// Convert enode to libp2p AddrInfo and connect.
		addrInfo, err := enodeToAddrInfo(node)
		if err != nil || addrInfo == nil {
			continue
		}

		// Don't reconnect to already-connected peers.
		if h.Network().Connectedness(addrInfo.ID) == 1 {
			continue
		}

		connectCtx, cancel := context.WithTimeout(ctx, 10*time.Second)
		if err := h.Connect(connectCtx, *addrInfo); err != nil {
			slog.Debug("failed to connect", "peer", peerShort(addrInfo.ID), "error", err)
		} else {
			slog.Debug("connected to peer", "peer", peerShort(addrInfo.ID))
		}
		cancel()
	}
}

func peerShort(id peer.ID) string {
	s := id.String()
	if len(s) > 16 {
		return s[:16]
	}
	return s
}

func matchesForkDigest(node *enode.Node, forkDigest [4]byte) bool {
	var eth2Data []byte
	if err := node.Record().Load(enr.WithEntry(eth2EnrKey, &eth2Data)); err != nil {
		return false
	}

	if len(eth2Data) < 4 {
		return false
	}

	var enrForkID ethpb.ENRForkID
	if err := enrForkID.UnmarshalSSZ(eth2Data); err != nil {
		return false
	}

	if len(enrForkID.CurrentForkDigest) < 4 {
		return false
	}
	return enrForkID.CurrentForkDigest[0] == forkDigest[0] &&
		enrForkID.CurrentForkDigest[1] == forkDigest[1] &&
		enrForkID.CurrentForkDigest[2] == forkDigest[2] &&
		enrForkID.CurrentForkDigest[3] == forkDigest[3]
}

func hasSubnetOverlap(node *enode.Node, subnetIDs []uint64) bool {
	attSubnetKey := params.BeaconNetworkConfig().AttSubnetKey
	var attnets []byte
	if err := node.Record().Load(enr.WithEntry(attSubnetKey, &attnets)); err != nil {
		return false
	}

	for _, id := range subnetIDs {
		if id < 64 && len(attnets) > int(id/8) {
			if attnets[id/8]&(1<<(id%8)) != 0 {
				return true
			}
		}
	}
	return false
}

func enodeToAddrInfo(node *enode.Node) (*peer.AddrInfo, error) {
	pubkey := node.Pubkey()
	if pubkey == nil {
		return nil, fmt.Errorf("no pubkey")
	}

	libp2pKey, err := ecdsaprysm.ConvertToInterfacePubkey(pubkey)
	if err != nil {
		return nil, fmt.Errorf("convert pubkey: %w", err)
	}

	peerID, err := peer.IDFromPublicKey(libp2pKey)
	if err != nil {
		return nil, fmt.Errorf("peer id: %w", err)
	}

	var multiaddrs []ma.Multiaddr
	ip := node.IP()
	if ip == nil {
		return nil, nil
	}

	// Try QUIC first (port from ENR "quic" key).
	var quicPort enr.UDP
	if err := node.Record().Load(&quicPort); err == nil && quicPort != 0 {
		addr, err := ma.NewMultiaddr(fmt.Sprintf("/ip4/%s/udp/%d/quic-v1", ip.String(), quicPort))
		if err == nil {
			multiaddrs = append(multiaddrs, addr)
		}
	}

	// TCP fallback.
	tcpPort := node.TCP()
	if tcpPort != 0 {
		addr, err := ma.NewMultiaddr(fmt.Sprintf("/ip4/%s/tcp/%d", ip.String(), tcpPort))
		if err == nil {
			multiaddrs = append(multiaddrs, addr)
		}
	}

	if len(multiaddrs) == 0 {
		return nil, nil
	}

	return &peer.AddrInfo{
		ID:    peerID,
		Addrs: multiaddrs,
	}, nil
}
