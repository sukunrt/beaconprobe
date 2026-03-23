package discovery

import (
	"context"
	"crypto/ecdsa"
	"fmt"
	"log/slog"
	"net"
	"strings"
	"sync/atomic"
	"time"

	"github.com/OffchainLabs/prysm/v7/config/params"
	ecdsaprysm "github.com/OffchainLabs/prysm/v7/crypto/ecdsa"
	ethpb "github.com/OffchainLabs/prysm/v7/proto/prysm/v1alpha1"
	"github.com/ethereum/go-ethereum/p2p/discover"
	"github.com/ethereum/go-ethereum/p2p/enode"
	"github.com/ethereum/go-ethereum/p2p/enr"
	"github.com/libp2p/go-libp2p/core/host"
	"github.com/libp2p/go-libp2p/core/network"
	"github.com/libp2p/go-libp2p/core/peer"
	ma "github.com/multiformats/go-multiaddr"
	"github.com/sukunrt/beaconprobe/metrics"
)

const (
	eth2EnrKey = "eth2"
)

// quicProtocol is the "quic" key, which holds the QUIC port of the node.
type quicProtocol uint16

func (quicProtocol) ENRKey() string { return "quic" }

// Config holds discovery configuration.
type Config struct {
	PrivKey      *ecdsa.PrivateKey
	DiscPort     uint
	DiscV4Port   uint // If non-zero, also run a discv4 scanner on this port.
	ForkDigest   [4]byte
	SubnetIDs    []uint64
	AttnetsBytes []byte
	QuicOnly     bool
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

	// Run discv5 peer discovery loop.
	go discoverPeers(ctx, h, listener.RandomNodes(), cfg)

	// Optionally start discv4 scanner.
	if cfg.DiscV4Port != 0 {
		v4Addr := &net.UDPAddr{IP: net.IPv4zero, Port: int(cfg.DiscV4Port)}
		v4Conn, err := net.ListenUDP("udp", v4Addr)
		if err != nil {
			return fmt.Errorf("listen udp v4: %w", err)
		}
		v4Listener, err := discover.ListenV4(v4Conn, localNode, dv5Cfg)
		if err != nil {
			return fmt.Errorf("listen discv4: %w", err)
		}
		slog.Info("discv4 listener started", "port", cfg.DiscV4Port)
		go discoverPeers(ctx, h, v4Listener.RandomNodes(), cfg)
	}

	return nil
}

const maxConcurrentDials = 500

func discoverPeers(ctx context.Context, h host.Host, iterator enode.Iterator, cfg Config) {
	defer iterator.Close()
	ticker := time.NewTicker(30 * time.Second)
	defer ticker.Stop()

	sem := make(chan struct{}, maxConcurrentDials)
	var inFlightDials atomic.Int64

	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			quic, tcp := countPeersByTransport(h)
			total := quic + tcp
			slog.Info("connected peers", "total", total, "quic", quic, "tcp", tcp, "in_flight_dials", inFlightDials.Load())
			metrics.ConnectedPeers.Set(float64(total))
			metrics.QUICPeers.Set(float64(quic))
			metrics.TCPPeers.Set(float64(tcp))
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

		// In quic-only mode, skip peers that have no QUIC address.
		if cfg.QuicOnly && !hasQUICAddr(addrInfo) {
			continue
		}

		// Don't reconnect to already-connected peers.
		if h.Network().Connectedness(addrInfo.ID) == network.Connected {
			continue
		}

		// Acquire semaphore slot.
		select {
		case sem <- struct{}{}:
		case <-ctx.Done():
			return
		}

		go func(ai peer.AddrInfo) {
			inFlightDials.Add(1)
			defer func() { inFlightDials.Add(-1); <-sem }()
			connectCtx, cancel := context.WithTimeout(ctx, 10*time.Second)
			defer cancel()
			if err := h.Connect(connectCtx, ai); err != nil {
				slog.Info("failed to connect", "peer", peerShort(ai.ID), "error", err)
			} else {
				conns := h.Network().ConnsToPeer(ai.ID)
				var remoteAddr string
				if len(conns) > 0 {
					remoteAddr = conns[0].RemoteMultiaddr().String()
				}
				slog.Info("connected to peer", "peer", peerShort(ai.ID), "remote_addr", remoteAddr)
			}
		}(*addrInfo)
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
	multiaddrs, err := retrieveMultiAddrsFromNode(node)
	if err != nil {
		return nil, err
	}
	if len(multiaddrs) == 0 {
		return nil, nil
	}
	infos, err := peer.AddrInfosFromP2pAddrs(multiaddrs...)
	if err != nil {
		return nil, fmt.Errorf("could not convert to peer info: %v: %w", multiaddrs, err)
	}
	if len(infos) != 1 {
		return nil, fmt.Errorf("infos contains %v elements, expected exactly 1", len(infos))
	}
	return &infos[0], nil
}

func retrieveMultiAddrsFromNode(node *enode.Node) ([]ma.Multiaddr, error) {
	multiaddrs := make([]ma.Multiaddr, 0, 2)

	pubkey := node.Pubkey()
	if pubkey == nil {
		return nil, fmt.Errorf("no pubkey")
	}
	assertedKey, err := ecdsaprysm.ConvertToInterfacePubkey(pubkey)
	if err != nil {
		return nil, fmt.Errorf("could not get pubkey: %w", err)
	}
	id, err := peer.IDFromPublicKey(assertedKey)
	if err != nil {
		return nil, fmt.Errorf("could not get peer id: %w", err)
	}

	ip := node.IP()
	if ip == nil {
		return nil, nil
	}
	ipType := "ip4"
	if ip.To4() == nil && ip.To16() != nil {
		ipType = "ip6"
	}

	// If the QUIC entry is present in the ENR, build the corresponding multiaddress.
	var qp quicProtocol
	if err := node.Load(&qp); err == nil && qp != 0 {
		addr, err := ma.NewMultiaddr(fmt.Sprintf("/%s/%s/udp/%d/quic-v1/p2p/%s", ipType, ip, qp, id))
		if err != nil {
			return nil, fmt.Errorf("could not build QUIC address: %w", err)
		}
		multiaddrs = append(multiaddrs, addr)
	}

	// If the TCP entry is present in the ENR, build the corresponding multiaddress.
	tcpPort := node.TCP()
	if tcpPort != 0 {
		addr, err := ma.NewMultiaddr(fmt.Sprintf("/%s/%s/tcp/%d/p2p/%s", ipType, ip, tcpPort, id))
		if err != nil {
			return nil, fmt.Errorf("could not build TCP address: %w", err)
		}
		multiaddrs = append(multiaddrs, addr)
	}

	return multiaddrs, nil
}

// hasQUICAddr returns true if the AddrInfo contains at least one QUIC address.
func hasQUICAddr(ai *peer.AddrInfo) bool {
	for _, addr := range ai.Addrs {
		if strings.Contains(addr.String(), "/quic-v1") {
			return true
		}
	}
	return false
}

// isQUICConn returns true if the connection uses the QUIC transport.
// RemoteMultiaddr() for QUIC connections is /ip4/x.x.x.x/udp/port (no /quic-v1 suffix),
// so we detect QUIC by checking for /udp/ and absence of /tcp/.
func isQUICConn(c network.Conn) bool {
	s := c.RemoteMultiaddr().String()
	return strings.Contains(s, "/udp/")
}

// countPeersByTransport returns the count of QUIC and TCP peers.
func countPeersByTransport(h host.Host) (quic, tcp int) {
	for _, p := range h.Network().Peers() {
		conns := h.Network().ConnsToPeer(p)
		isQUIC := false
		for _, c := range conns {
			if isQUICConn(c) {
				isQUIC = true
				break
			}
		}
		if isQUIC {
			quic++
		} else {
			tcp++
		}
	}
	return
}
