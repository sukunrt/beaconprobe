package main

import (
	"context"
	"encoding/hex"
	"flag"
	"fmt"
	"log/slog"
	"os"
	"os/signal"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"time"

	"github.com/OffchainLabs/prysm/v7/config/params"
	"github.com/OffchainLabs/prysm/v7/time/slots"
	"github.com/sukunrt/beaconprobe/discovery"
	"github.com/sukunrt/beaconprobe/metrics"
	"github.com/sukunrt/beaconprobe/node"
	"github.com/sukunrt/beaconprobe/rpc"
)

func main() {
	tcpPort := flag.Uint("tcp-port", 9020, "libp2p TCP listen port")
	quicPort := flag.Uint("quic-port", 9021, "libp2p QUIC listen port")
	discPort := flag.Uint("discv5-port", 9022, "discv5 UDP listen port")
	discV4Port := flag.Uint("discv4-port", 9023, "discv4 UDP listen port (0 to disable)")
	subnetsFlag := flag.String("subnets", "0,1,2,3", "comma-separated attestation subnet IDs")
	metricsAddr := flag.String("metrics-addr", ":9090", "Prometheus metrics listen address")
	logLevel := flag.String("log-level", "info", "log level (debug, info, warn, error)")
	logFilePath := flag.String("log-file-path", "", "file path to log attestation arrival times")
	gossipD := flag.Int("gossip-d", 8, "gossipsub mesh degree (set high e.g. 10000 for observer mode)")
	keyFile := flag.String("key-file", "", "path to persist node private key (reuse peer ID across restarts)")
	quicOnly := flag.Bool("quic-only", false, "only use QUIC transport (disable TCP/yamux entirely)")
	disableIHave := flag.Bool("disable-ihave", false, "disable gossipsub IHAVE gossip")
	crawlFile := flag.String("crawl", "", "crawl mode: discover all peers and write ENRs to this file")
	bootstrapFile := flag.String("bootstrap-file", "", "path to ENR file for direct-dialing peers at startup")
	listenBlocks := flag.Bool("listen-blocks", true, "subscribe to beacon block topic and measure attestation latency relative to block arrival")
	flag.Parse()

	if *crawlFile != "" && *bootstrapFile != "" {
		slog.Error("--crawl and --bootstrap-file are mutually exclusive")
		os.Exit(1)
	}

	var slogLevel slog.Level
	if err := slogLevel.UnmarshalText([]byte(*logLevel)); err != nil {
		slog.Error("invalid log level", "error", err)
		os.Exit(1)
	}
	slog.SetDefault(slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{
		Level:       slogLevel,
		ReplaceAttr: replaceTimeAttr,
	})))

	// Parse subnet IDs.
	subnetIDs, err := parseSubnets(*subnetsFlag)
	if err != nil {
		slog.Error("invalid subnets flag", "error", err)
		os.Exit(1)
	}

	// In crawl mode, advertise all 64 subnets to attract maximum peers.
	if *crawlFile != "" {
		subnetIDs = make([]uint64, 64)
		for i := range subnetIDs {
			subnetIDs[i] = uint64(i)
		}
	}

	// Use mainnet config.
	// Actual mainnet genesis is 23s after MinGenesisTime (waited for validator threshold).
	cfg := params.BeaconConfig()
	const mainnetGenesisUnix = 1606824023
	genesisTime := time.Unix(mainnetGenesisUnix, 0)

	// Compute current slot and epoch.
	currentSlot := slots.CurrentSlot(genesisTime)
	currentEpoch := slots.ToEpoch(currentSlot)
	forkDigest := params.ForkDigest(currentEpoch)

	slog.Info("beaconprobe starting",
		"genesisTime", genesisTime,
		"currentSlot", currentSlot,
		"currentEpoch", currentEpoch,
		"forkDigest", hex.EncodeToString(forkDigest[:]),
		"subnets", subnetIDs,
	)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	// 1. Start Prometheus metrics server.
	go metrics.Serve(*metricsAddr)

	// 2. Create libp2p host.
	h, privKey, err := node.NewHost(*tcpPort, *quicPort, *keyFile, *quicOnly)
	if err != nil {
		slog.Error("failed to create libp2p host", "error", err)
		os.Exit(1)
	}
	slog.Info("libp2p host created", "peerID", h.ID().String())
	for _, addr := range h.Addrs() {
		slog.Info("listening", "addr", addr.String())
	}

	// 3. Track peer user agents via identify events.
	go node.TrackUserAgents(ctx, h)

	// 4. Register RPC handlers (before discovery so peers can handshake).
	attnetsBytes := rpc.MakeAttnetsBytes(subnetIDs)
	statusProvider := rpc.MakeStatusProvider(forkDigest)
	rpc.RegisterHandlers(h, statusProvider, attnetsBytes)
	rpc.SendStatusOnConnect(h, statusProvider)
	slog.Info("RPC handlers registered")

	// 5. Gossipsub and attestation listening (skipped in crawl mode).
	if *crawlFile == "" {
		if *logFilePath == "" {
			slog.Error("--log-file-path is required")
			os.Exit(1)
		}
		genesisValRoot := cfg.GenesisValidatorsRoot[:]
		gossipLogFile := filepath.Join(filepath.Dir(*logFilePath), "gossipsub-logs.log")
		ps, err := node.NewGossipSub(ctx, h, genesisValRoot, *gossipD, *disableIHave, forkDigest, subnetIDs, gossipLogFile)
		if err != nil {
			slog.Error("failed to create gossipsub", "error", err)
			os.Exit(1)
		}

		subs, err := node.SubscribeSubnets(ps, forkDigest, subnetIDs)
		if err != nil {
			slog.Error("failed to subscribe to subnets", "error", err)
			os.Exit(1)
		}
		for _, s := range subs {
			slog.Info("subscribed to subnet",
				"subnet", s.SubnetID,
				"topic", node.SubnetTopic(forkDigest, s.SubnetID),
			)
		}

		// Set up file logger.
		f, err := os.OpenFile(*logFilePath, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0644)
		if err != nil {
			slog.Error("failed to open log file", "path", *logFilePath, "error", err)
			os.Exit(1)
		}
		defer f.Close()
		fileLogger := slog.New(slog.NewTextHandler(f, &slog.HandlerOptions{
			ReplaceAttr: replaceTimeAttr,
		}))
		slog.Info("logging to file", "path", *logFilePath)

		// Subscribe to block topic and start block listener if enabled.
		var blockTracker *node.BlockTracker
		if *listenBlocks {
			blockSub, err := node.SubscribeBlocks(ps, forkDigest)
			if err != nil {
				slog.Error("failed to subscribe to block topic", "error", err)
				os.Exit(1)
			}
			slog.Info("subscribed to block topic", "topic", node.BlockTopic(forkDigest))
			blockTracker = node.NewBlockTracker()
			go node.ListenForBlocks(ctx, blockSub, genesisTime, blockTracker, fileLogger)
		}

		// Start attestation listener goroutines.
		node.ListenForAttestations(ctx, subs, genesisTime, blockTracker, fileLogger)
	} else {
		slog.Info("crawl mode: skipping gossipsub and attestation listener")
	}

	// 6. Start peer manager (skipped in crawl mode).
	var pm *node.PeerManager
	if *crawlFile == "" {
		pm = node.NewPeerManager(h, *gossipD)
		go pm.Run(ctx)
		slog.Info("peer manager started", "minPeers", 7*(*gossipD), "maxPeers", 10*(*gossipD))
	}

	// 7. Start discv5 discovery + peer connection loop.
	discCfg := discovery.Config{
		PrivKey:      privKey,
		DiscPort:     *discPort,
		ForkDigest:   forkDigest,
		SubnetIDs:    subnetIDs,
		AttnetsBytes: attnetsBytes,
		DiscV4Port:   *discV4Port,
		QuicOnly:     *quicOnly,
		CrawlFile:    *crawlFile,
	}
	if pm != nil {
		discCfg.Candidates = pm.Candidates
	}
	discv5Listener, err := discovery.StartDiscovery(ctx, h, discCfg)
	if err != nil {
		slog.Error("failed to start discovery", "error", err)
		os.Exit(1)
	}

	// 7b. If bootstrap file provided, refresh ENRs via discv5 and send to peer manager.
	if *bootstrapFile != "" {
		go func() {
			for {
				discovery.DialBootstrapPeers(ctx, h, discv5Listener, *bootstrapFile, forkDigest, subnetIDs, *quicOnly, pm.Candidates)
				time.Sleep(60 * time.Second)
			}
		}()
	}

	// 7. Wait for shutdown signal.
	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, syscall.SIGINT, syscall.SIGTERM)
	sig := <-sigCh
	slog.Info("shutting down", "signal", sig)
	cancel()
	if err := h.Close(); err != nil {
		slog.Error("error closing host", "error", err)
	}
}

func replaceTimeAttr(_ []string, a slog.Attr) slog.Attr {
	if a.Key == slog.TimeKey {
		a.Value = slog.StringValue(a.Value.Time().Format("2006-01-02T15:04:05.000000Z07:00"))
	}
	return a
}

func parseSubnets(s string) ([]uint64, error) {
	parts := strings.Split(s, ",")
	ids := make([]uint64, 0, len(parts))
	for _, p := range parts {
		p = strings.TrimSpace(p)
		if p == "" {
			continue
		}
		id, err := strconv.ParseUint(p, 10, 64)
		if err != nil {
			return nil, fmt.Errorf("parse subnet ID %q: %w", p, err)
		}
		ids = append(ids, id)
	}
	return ids, nil
}
