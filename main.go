package main

import (
	"context"
	"crypto/ecdsa"
	"encoding/hex"
	"flag"
	"log/slog"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/OffchainLabs/prysm/v7/config/params"
	"github.com/OffchainLabs/prysm/v7/time/slots"
	gcrypto "github.com/ethereum/go-ethereum/crypto"
	"github.com/ethereum/go-ethereum/p2p/discover"
	"github.com/libp2p/go-libp2p/core/crypto"
	"github.com/libp2p/go-libp2p/core/host"
	"github.com/libp2p/go-libp2p/core/peer"
	"github.com/sukunrt/beaconprobe/config"
	"github.com/sukunrt/beaconprobe/discovery"
	"github.com/sukunrt/beaconprobe/metrics"
	"github.com/sukunrt/beaconprobe/node"
	"github.com/sukunrt/beaconprobe/rpc"
)

func main() {
	configPath := flag.String("config", "", "path to YAML config file")
	flag.Parse()

	if *configPath == "" {
		slog.Error("--config is required")
		os.Exit(1)
	}

	cfg, err := config.Load(*configPath)
	if err != nil {
		slog.Error("failed to load config", "error", err)
		os.Exit(1)
	}

	// Set up logging.
	var slogLevel slog.Level
	if err := slogLevel.UnmarshalText([]byte(cfg.LogLevel)); err != nil {
		slog.Error("invalid log level", "error", err)
		os.Exit(1)
	}
	slog.SetDefault(slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{
		Level:       slogLevel,
		ReplaceAttr: replaceTimeAttr,
	})))

	// Mainnet genesis.
	beaconCfg := params.BeaconConfig()
	const mainnetGenesisUnix = 1606824023
	genesisTime := time.Unix(mainnetGenesisUnix, 0)

	currentSlot := slots.CurrentSlot(genesisTime)
	currentEpoch := slots.ToEpoch(currentSlot)
	forkDigest := params.ForkDigest(currentEpoch)

	subnetIDs := cfg.Subnets
	if cfg.Mode == "crawl" {
		subnetIDs = make([]uint64, 64)
		for i := range subnetIDs {
			subnetIDs[i] = uint64(i)
		}
	}

	slog.Info("beaconprobe starting",
		"mode", cfg.Mode,
		"instances", len(cfg.Instances),
		"genesisTime", genesisTime,
		"currentSlot", currentSlot,
		"currentEpoch", currentEpoch,
		"forkDigest", hex.EncodeToString(forkDigest[:]),
		"subnets", subnetIDs,
	)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	go metrics.Serve(cfg.MetricsAddr)

	attnetsBytes := rpc.MakeAttnetsBytes(subnetIDs)
	statusProvider := rpc.MakeStatusProvider(forkDigest, genesisTime)

	var cleanup func()
	if cfg.Mode == "crawl" {
		cleanup = runCrawl(ctx, cfg, forkDigest, subnetIDs, attnetsBytes, statusProvider, genesisTime)
	} else {
		cleanup = runProbe(ctx, cfg, beaconCfg, forkDigest, subnetIDs, attnetsBytes, statusProvider, genesisTime)
	}

	// Wait for shutdown signal.
	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, syscall.SIGINT, syscall.SIGTERM)
	sig := <-sigCh
	slog.Info("shutting down", "signal", sig)
	cancel()
	if cleanup != nil {
		cleanup()
	}
}

// runCrawl sets up a single-instance crawl mode (no gossipsub, no peer cap).
// Returns a cleanup function to close the host.
func runCrawl(
	ctx context.Context,
	cfg *config.Config,
	forkDigest [4]byte,
	subnetIDs []uint64,
	attnetsBytes []byte,
	statusProvider rpc.StatusProvider,
	genesisTime time.Time,
) func() {
	inst := cfg.Instances[0]
	m := metrics.NewMetrics(inst.Name)

	h, privKey, err := node.NewHost(cfg.TCPPort(0), cfg.QUICPort(0), cfg.KeyFile(0), inst.QuicOnly)
	if err != nil {
		slog.Error("failed to create libp2p host", "error", err)
		os.Exit(1)
	}
	logHost(h, inst.Name)

	go node.TrackUserAgents(ctx, h, m)
	rpc.RegisterHandlers(h, statusProvider, attnetsBytes)
	rpc.SendStatusOnConnect(h, statusProvider)

	pm := node.NewPeerManager(h, 0, m) // no peer cap
	go pm.Run(ctx)
	go node.ReportConnectivity(ctx, h, m)

	discCfg := discovery.Config{
		PrivKey:      privKey,
		DiscPort:     cfg.DiscV5Port(0),
		ForkDigest:   forkDigest,
		SubnetIDs:    subnetIDs,
		AttnetsBytes: attnetsBytes,
		DiscV4Port:   cfg.DiscV4Port,
		QuicOnly:     inst.QuicOnly,
		ActiveCrawl:  true,
		CrawlFile:    cfg.CrawlFile,
		Candidates:   pm.Candidates,
	}
	if _, err := discovery.StartDiscovery(ctx, h, discCfg); err != nil {
		slog.Error("failed to start discovery", "error", err)
		os.Exit(1)
	}

	slog.Info("crawl mode started")
	return func() { h.Close() }
}

// runProbe sets up N instances with disjoint peer sets, shared block tracker, and per-instance metrics.
// Returns a cleanup function to close hosts and log files.
func runProbe(
	ctx context.Context,
	cfg *config.Config,
	beaconCfg *params.BeaconChainConfig,
	forkDigest [4]byte,
	subnetIDs []uint64,
	attnetsBytes []byte,
	statusProvider rpc.StatusProvider,
	genesisTime time.Time,
) func() {
	n := len(cfg.Instances)
	instanceNames := make([]string, n)
	for i, inst := range cfg.Instances {
		instanceNames[i] = inst.Name
	}

	// 2. Pre-load all keys and derive peer IDs.
	keys := make([]*ecdsa.PrivateKey, n)
	peerIDs := make([]peer.ID, n)
	for i := range cfg.Instances {
		key, err := node.LoadOrGenerateKey(cfg.KeyFile(i))
		if err != nil {
			slog.Error("failed to load key", "instance", instanceNames[i], "error", err)
			os.Exit(1)
		}
		keys[i] = key
		pid, err := peerIDFromECDSA(key)
		if err != nil {
			slog.Error("failed to derive peer ID", "instance", instanceNames[i], "error", err)
			os.Exit(1)
		}
		peerIDs[i] = pid
	}

	// 3. Create SharedPeerSet and shared BlockTracker.
	peerSet := node.NewSharedPeerSet(instanceNames)
	blockTracker := node.NewBlockTracker()

	// 4. Create all instances.
	hosts := make(map[string]host.Host, n)
	candidateChans := make(map[string]chan<- peer.AddrInfo, n)
	var logFiles []*os.File
	for i, inst := range cfg.Instances {
		name := inst.Name

		// Build sibling IDs (all peer IDs except this instance).
		siblingIDs := make([]peer.ID, 0, n-1)
		for j, pid := range peerIDs {
			if j != i {
				siblingIDs = append(siblingIDs, pid)
			}
		}

		// Create gater and host.
		gater := node.NewSiblingGater(name, siblingIDs, peerSet)
		h, err := node.NewHostWithKey(cfg.TCPPort(i), cfg.QUICPort(i), keys[i], inst.QuicOnly, gater)
		if err != nil {
			slog.Error("failed to create host", "instance", name, "error", err)
			os.Exit(1)
		}
		logHost(h, name)
		hosts[name] = h
		// Per-instance metrics.
		m := metrics.NewMetrics(name)

		// RPC handlers.
		rpc.RegisterHandlers(h, statusProvider, attnetsBytes)
		rpc.SendStatusOnConnect(h, statusProvider)

		// User agent tracking.
		go node.TrackUserAgents(ctx, h, m)

		// Peer manager.
		pm := node.NewPeerManager(h, inst.MaxPeers, m)
		go pm.Run(ctx)
		candidateChans[name] = pm.Candidates

		// Connectivity reporting.
		go node.ReportConnectivity(ctx, h, m)

		// Inbound peer enforcement.
		go node.HandleInboundPeers(ctx, h, name, peerSet)

		// GossipSub + subscriptions.
		genesisValRoot := beaconCfg.GenesisValidatorsRoot[:]
		gossipLogFile := inst.GossipSubLogFile
		if gossipLogFile == "" {
			gossipLogFile = inst.LogFile + ".gossipsub"
		}
		ps, err := node.NewGossipSub(ctx, h, genesisValRoot, inst.GossipD, inst.DisableIHave, forkDigest, subnetIDs, gossipLogFile)
		if err != nil {
			slog.Error("failed to create gossipsub", "instance", name, "error", err)
			os.Exit(1)
		}

		subs, err := node.SubscribeSubnets(ps, forkDigest, subnetIDs)
		if err != nil {
			slog.Error("failed to subscribe to subnets", "instance", name, "error", err)
			os.Exit(1)
		}
		for _, s := range subs {
			slog.Info("subscribed to subnet",
				"instance", name,
				"subnet", s.SubnetID,
				"topic", node.SubnetTopic(forkDigest, s.SubnetID),
			)
		}

		// File logger for attestations.
		f, err := os.OpenFile(inst.LogFile, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0644)
		if err != nil {
			slog.Error("failed to open log file", "instance", name, "path", inst.LogFile, "error", err)
			os.Exit(1)
		}
		logFiles = append(logFiles, f)
		fileLogger := slog.New(slog.NewTextHandler(f, &slog.HandlerOptions{
			ReplaceAttr: replaceTimeAttr,
		}))

		// Only the first instance subscribes to blocks and runs the block listener.
		if i == 0 && cfg.ListenBlocks {
			blockSub, err := node.SubscribeBlocks(ps, forkDigest)
			if err != nil {
				slog.Error("failed to subscribe to block topic", "instance", name, "error", err)
				os.Exit(1)
			}
			slog.Info("subscribed to block topic", "instance", name, "topic", node.BlockTopic(forkDigest))
			go node.ListenForBlocks(ctx, blockSub, genesisTime, blockTracker, fileLogger, m)
		}

		// Attestation listeners.
		node.ListenForAttestations(ctx, subs, genesisTime, blockTracker, fileLogger, m)
	}

	// 5. Create router.
	router := node.NewMultiInstanceRouter(peerSet, candidateChans, hosts)

	// 6. Start discovery for each instance.
	// Only the first instance actively crawls; others just serve their discv5 ENR.
	var firstListener *discover.UDPv5
	for i, inst := range cfg.Instances {
		discCfg := discovery.Config{
			PrivKey:      keys[i],
			DiscPort:     cfg.DiscV5Port(i),
			ForkDigest:   forkDigest,
			SubnetIDs:    subnetIDs,
			AttnetsBytes: attnetsBytes,
			QuicOnly:     inst.QuicOnly,
			ActiveCrawl:  i == 0,
			Router:       router,
		}
		if i == 0 {
			discCfg.DiscV4Port = cfg.DiscV4Port
		}
		listener, err := discovery.StartDiscovery(ctx, hosts[inst.Name], discCfg)
		if err != nil {
			slog.Error("failed to start discovery", "instance", inst.Name, "error", err)
			os.Exit(1)
		}
		if i == 0 {
			firstListener = listener
		}
		slog.Info("discovery started", "instance", inst.Name, "active_crawl", i == 0)
	}

	// 7. Bootstrap file (if provided).
	if cfg.BootstrapFile != "" {
		go func() {
			for {
				discovery.DialBootstrapPeers(ctx, firstListener, router, cfg.BootstrapFile, forkDigest, subnetIDs, cfg.Instances[0].QuicOnly)
				select {
				case <-ctx.Done():
					return
				case <-time.After(60 * time.Second):
				}
			}
		}()
	}

	return func() {
		for _, h := range hosts {
			h.Close()
		}
		for _, f := range logFiles {
			f.Close()
		}
	}
}

func logHost(h host.Host, name string) {
	slog.Info("host created", "instance", name, "peerID", h.ID().String())
	for _, addr := range h.Addrs() {
		slog.Info("listening", "instance", name, "addr", addr.String())
	}
}

func peerIDFromECDSA(key *ecdsa.PrivateKey) (peer.ID, error) {
	libp2pKey, err := crypto.UnmarshalSecp256k1PrivateKey(gcrypto.FromECDSA(key))
	if err != nil {
		return "", err
	}
	return peer.IDFromPrivateKey(libp2pKey)
}

func replaceTimeAttr(_ []string, a slog.Attr) slog.Attr {
	if a.Key == slog.TimeKey {
		a.Value = slog.StringValue(a.Value.Time().Format("2006-01-02T15:04:05.000000Z07:00"))
	}
	return a
}
