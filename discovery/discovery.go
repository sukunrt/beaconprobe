package discovery

import (
	"bufio"
	"context"
	"crypto/ecdsa"
	"fmt"
	"log/slog"
	"math/rand/v2"
	"net"
	"os"
	"strings"
	"sync"

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

// PeerRouter routes discovered peers to the appropriate instance's PeerManager.
type PeerRouter interface {
	// Route assigns the peer to an instance and sends it to the corresponding PeerManager.
	Route(ai peer.AddrInfo)
	// IsConnectedToAny returns true if any instance is already connected to the peer.
	IsConnectedToAny(id peer.ID) bool
}

// Config holds discovery configuration.
type Config struct {
	PrivKey      *ecdsa.PrivateKey
	DiscPort     uint
	DiscV4Port   uint // If non-zero, also run a discv4 scanner on this port.
	ForkDigest   [4]byte
	SubnetIDs    []uint64
	AttnetsBytes []byte
	QuicOnly     bool
	ActiveCrawl  bool                 // If true, spawn discoverPeers loop. Only first instance sets this.
	CrawlFile    string               // If non-empty, run in crawl mode and write ENRs to this file.
	Router PeerRouter // Routes discovered peers to instances.
}

// StartDiscovery starts discv5 and a peer connection loop.
// It returns the discv5 listener so callers can use it for ENR lookups.
func StartDiscovery(ctx context.Context, h host.Host, cfg Config) (*discover.UDPv5, error) {
	// Create local ENR node.
	db, err := enode.OpenDB("")
	if err != nil {
		return nil, fmt.Errorf("open enode db: %w", err)
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
		return nil, fmt.Errorf("marshal ENR fork ID: %w", err)
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
		return nil, fmt.Errorf("listen udp: %w", err)
	}

	// Start discv5 listener.
	dv5Cfg := discover.Config{
		PrivateKey: cfg.PrivKey,
		Bootnodes:  bootNodes,
	}

	listener, err := discover.ListenV5(conn, localNode, dv5Cfg)
	if err != nil {
		return nil, fmt.Errorf("listen discv5: %w", err)
	}
	slog.Info("discv5 listener started")

	// Only the active crawler spawns the discoverPeers loop.
	// Other instances just serve their discv5 ENR without crawling.
	if cfg.ActiveCrawl {
		forkFilter := enode.Filter(listener.RandomNodes(), func(n *enode.Node) bool {
			return matchesForkDigest(n, cfg.ForkDigest)
		})

		// Crawl mode: open file for appending ENRs and track seen peers.
		// Register a network notifee so ENRs are written when PeerManager connects.
		var crawlWriter *crawlFileWriter
		if cfg.CrawlFile != "" {
			var err error
			crawlWriter, err = newCrawlFileWriter(cfg.CrawlFile)
			if err != nil {
				slog.Error("failed to open crawl file", "path", cfg.CrawlFile, "error", err)
				return nil, err
			}
			h.Network().Notify(&network.NotifyBundle{
				ConnectedF: func(_ network.Network, conn network.Conn) {
					crawlWriter.WriteConnected(conn.RemotePeer())
				},
			})
		}
		go discoverPeers(ctx, forkFilter, cfg, crawlWriter)

		// Optionally start discv4 scanner.
		if cfg.DiscV4Port != 0 {
			v4Addr := &net.UDPAddr{IP: net.IPv4zero, Port: int(cfg.DiscV4Port)}
			v4Conn, err := net.ListenUDP("udp", v4Addr)
			if err != nil {
				return nil, fmt.Errorf("listen udp v4: %w", err)
			}
			v4Listener, err := discover.ListenV4(v4Conn, localNode, dv5Cfg)
			if err != nil {
				return nil, fmt.Errorf("listen discv4: %w", err)
			}
			slog.Info("discv4 listener started", "port", cfg.DiscV4Port)
			v4ForkFilter := enode.Filter(v4Listener.RandomNodes(), func(n *enode.Node) bool {
				return matchesForkDigest(n, cfg.ForkDigest)
			})
			go discoverPeers(ctx, v4ForkFilter, cfg, crawlWriter)
		}
	}

	return listener, nil
}

func discoverPeers(ctx context.Context, iterator enode.Iterator, cfg Config, crawlWriter *crawlFileWriter) {
	defer iterator.Close()

	for {
		select {
		case <-ctx.Done():
			return
		default:
		}

		if !iterator.Next() {
			return
		}
		node := iterator.Node()
		metrics.DiscoveryPeersFound.Inc()

		// In crawl mode, skip subnet overlap check to discover all peers.
		if cfg.CrawlFile == "" {
			if !hasSubnetOverlap(node, cfg.SubnetIDs) {
				continue
			}
		}

		addrInfo, err := enodeToAddrInfo(node)
		if err != nil || addrInfo == nil {
			continue
		}

		if cfg.QuicOnly && !hasQUICAddr(addrInfo) {
			continue
		}

		// Check if already connected to any instance.
		if cfg.Router.IsConnectedToAny(addrInfo.ID) {
			continue
		}

		// In crawl mode, register the ENR before sending to PeerManager.
		if crawlWriter != nil {
			if crawlWriter.HasPeer(addrInfo.ID) {
				continue
			}
			crawlWriter.RegisterENR(addrInfo.ID, node.String())
		}

		cfg.Router.Route(*addrInfo)
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

// crawlFileWriter writes ENRs to a file with deduplication.
// Discovery calls RegisterENR when sending a candidate to PeerManager,
// and WriteConnected is called via a network notifee when the peer connects.
type crawlFileWriter struct {
	mu      sync.Mutex
	f       *os.File
	w       *bufio.Writer
	seen    map[peer.ID]struct{}
	pending map[peer.ID]string // peer ID -> ENR string, awaiting connection
}

func newCrawlFileWriter(path string) (*crawlFileWriter, error) {
	f, err := os.OpenFile(path, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0644)
	if err != nil {
		return nil, err
	}
	return &crawlFileWriter{
		f:       f,
		w:       bufio.NewWriter(f),
		seen:    make(map[peer.ID]struct{}),
		pending: make(map[peer.ID]string),
	}, nil
}

func (c *crawlFileWriter) HasPeer(id peer.ID) bool {
	c.mu.Lock()
	defer c.mu.Unlock()
	_, ok := c.seen[id]
	if !ok {
		_, ok = c.pending[id]
	}
	return ok
}

// RegisterENR stores the ENR for a peer that has been sent to PeerManager for dialing.
func (c *crawlFileWriter) RegisterENR(id peer.ID, enrStr string) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.pending[id] = enrStr
}

// WriteConnected writes the ENR for a peer that has successfully connected.
func (c *crawlFileWriter) WriteConnected(id peer.ID) {
	c.mu.Lock()
	defer c.mu.Unlock()
	enrStr, ok := c.pending[id]
	if !ok {
		return
	}
	delete(c.pending, id)
	if _, ok := c.seen[id]; ok {
		return
	}
	c.seen[id] = struct{}{}
	fmt.Fprintln(c.w, enrStr)
	c.w.Flush()
	slog.Info("crawl: wrote ENR", "peer", peerShort(id), "total", len(c.seen))
}

func (c *crawlFileWriter) Close() error {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.w.Flush()
	return c.f.Close()
}

// RouteBootstrapPeers reads ENRs from a file, refreshes each via discv5
// RequestENR to get current subnet subscriptions, filters by subnet overlap,
// and routes matching peers via the PeerRouter.
func RouteBootstrapPeers(
	ctx context.Context,
	listener *discover.UDPv5,
	router PeerRouter,
	filePath string,
	forkDigest [4]byte,
	subnetIDs []uint64,
	quicOnly bool,
) {
	f, err := os.Open(filePath)
	if err != nil {
		slog.Error("failed to open bootstrap file", "path", filePath, "error", err)
		return
	}
	defer f.Close()

	var nodeStrings []string
	scanner := bufio.NewScanner(f)
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" {
			continue
		}
		nodeStrings = append(nodeStrings, line)
	}
	rand.Shuffle(len(nodeStrings),
		func(i, j int) { nodeStrings[i], nodeStrings[j] = nodeStrings[j], nodeStrings[i] })

	for _, line := range nodeStrings {
		if ctx.Err() != nil {
			return
		}
		node, err := enode.Parse(enode.ValidSchemes, line)
		if err != nil {
			slog.Info("bootstrap: failed to parse ENR", "error", err)
			continue
		}

		// Refresh ENR via discv5 to get current subnet subscriptions.
		freshNode, err := listener.RequestENR(node)
		if err != nil {
			slog.Info("bootstrap: failed to refresh ENR", "peer", node.ID().TerminalString(), "error", err)
			freshNode = node
		}

		if !matchesForkDigest(freshNode, forkDigest) {
			continue
		}

		if !hasSubnetOverlap(freshNode, subnetIDs) {
			continue
		}

		addrInfo, err := enodeToAddrInfo(freshNode)
		if err != nil || addrInfo == nil {
			continue
		}

		if quicOnly && !hasQUICAddr(addrInfo) {
			continue
		}

		if router.IsConnectedToAny(addrInfo.ID) {
			continue
		}

		router.Route(*addrInfo)
	}
	slog.Info("bootstrap: finished reading file")
}
