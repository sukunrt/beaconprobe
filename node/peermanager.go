package node

import (
	"context"
	"fmt"
	"log/slog"
	"sync"
	"sync/atomic"
	"time"

	"github.com/libp2p/go-libp2p/core/host"
	"github.com/libp2p/go-libp2p/core/network"
	"github.com/libp2p/go-libp2p/core/peer"

	"github.com/sukunrt/beaconprobe/metrics"
)

const (
	maxConcurrentDials = 500
	dialTimeout        = 4 * time.Second
	initialBackoff     = 30 * time.Second
	maxBackoff         = 30 * time.Minute
	peerCheckInterval  = 10 * time.Second
)

type backoffEntry struct {
	failCount int
	nextDial  time.Time
}

// PeerManager manages peer connections with backpressure, backoff, and peer caps.
type PeerManager struct {
	h          host.Host
	maxPeers   int // 0 = no cap (crawl mode)
	Candidates chan peer.AddrInfo
	m          *metrics.Metrics

	mu      sync.Mutex
	backoff map[peer.ID]backoffEntry
}

// NewPeerManager creates a PeerManager. maxPeers=0 means no peer cap (crawl mode).
func NewPeerManager(h host.Host, maxPeers int, m *metrics.Metrics) *PeerManager {
	return &PeerManager{
		h:          h,
		maxPeers:   maxPeers,
		Candidates: make(chan peer.AddrInfo, 64),
		backoff:    make(map[peer.ID]backoffEntry),
		m:          m,
	}
}

// Run processes candidates until ctx is cancelled.
func (pm *PeerManager) Run(ctx context.Context) {
	sem := make(chan struct{}, maxConcurrentDials)
	var inFlight atomic.Int64

	for {
		// Gate: if peers + in-flight dials >= maxPeers + 20, wait for ticker.
		if pm.tooManyInFlight(&inFlight) {
			select {
			case <-ctx.Done():
				return
			case <-time.After(10 * time.Second):
				continue
			}
		}

		select {
		case <-ctx.Done():
			return
		case ai := <-pm.Candidates:
			select {
			case sem <- struct{}{}:
			case <-ctx.Done():
				return
			}
			inFlight.Add(1)
			go func() {
				defer func() { <-sem; inFlight.Add(-1) }()
				pm.handleCandidate(ctx, ai)
			}()
			if pm.tooManyInFlight(&inFlight) {
				continue
			}
		}
	}
}

func (pm *PeerManager) tooManyInFlight(inFlight *atomic.Int64) bool {
	if pm.maxPeers == 0 {
		return false
	}
	return len(pm.h.Network().Peers())+int(inFlight.Load()) >= pm.maxPeers+20
}

func (pm *PeerManager) handleCandidate(ctx context.Context, ai peer.AddrInfo) {
	// Already connected.
	if pm.h.Network().Connectedness(ai.ID) == network.Connected {
		return
	}

	// Check backoff.
	if pm.inBackoff(ai.ID) {
		pm.m.DialBackoffs.Inc()
		return
	}

	// Check peer cap.
	if pm.maxPeers > 0 && len(pm.h.Network().Peers()) >= pm.maxPeers {
		return
	}

	pm.m.DialAttempts.Inc()
	connectCtx, cancel := context.WithTimeout(ctx, dialTimeout)
	defer cancel()
	connectCtx = network.WithDialPeerTimeout(connectCtx, dialTimeout)

	if err := pm.h.Connect(connectCtx, ai); err != nil {
		pm.recordFailure(ai.ID)
		slog.Info("failed to connect", "peer", ai.ID.String()[:16], "error", err)
	} else {
		pm.clearBackoff(ai.ID)
		conns := pm.h.Network().ConnsToPeer(ai.ID)
		var remoteAddr string
		if len(conns) > 0 {
			remoteAddr = conns[0].RemoteMultiaddr().String()
		}
		slog.Info("connected to peer", "peer", ai.ID.String()[:16], "remote_addr", remoteAddr)
	}
}

func (pm *PeerManager) inBackoff(id peer.ID) bool {
	pm.mu.Lock()
	defer pm.mu.Unlock()
	entry, ok := pm.backoff[id]
	if !ok {
		return false
	}
	return time.Now().Before(entry.nextDial)
}

func (pm *PeerManager) recordFailure(id peer.ID) {
	pm.mu.Lock()
	defer pm.mu.Unlock()
	entry := pm.backoff[id]
	entry.failCount++
	backoff := min(initialBackoff*(1<<min(entry.failCount-1, 10)), maxBackoff)
	entry.nextDial = time.Now().Add(backoff)
	pm.backoff[id] = entry
	slog.Info("peer backoff",
		"peer", id.String()[:16],
		"failures", entry.failCount,
		"next_dial_in", backoff.Round(time.Second),
	)
}

func (pm *PeerManager) clearBackoff(id peer.ID) {
	pm.mu.Lock()
	defer pm.mu.Unlock()
	delete(pm.backoff, id)
}

// BackoffDuration returns the current backoff duration for a peer, or 0 if not in backoff.
func (pm *PeerManager) BackoffDuration(id peer.ID) time.Duration {
	pm.mu.Lock()
	defer pm.mu.Unlock()
	entry, ok := pm.backoff[id]
	if !ok {
		return 0
	}
	d := time.Until(entry.nextDial)
	if d < 0 {
		return 0
	}
	return d
}

// Status returns a summary string for logging.
func (pm *PeerManager) Status() string {
	peerCount := len(pm.h.Network().Peers())
	pm.mu.Lock()
	backoffCount := len(pm.backoff)
	pm.mu.Unlock()
	return fmt.Sprintf("peers=%d/%d backoff=%d", peerCount, pm.maxPeers, backoffCount)
}
