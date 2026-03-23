# AGENTS.md

## Project Overview

Beaconprobe is a passive Ethereum beacon chain observer. It joins the eth2 P2P network via libp2p, subscribes to attestation subnet gossipsub topics, and measures attestation arrival latency relative to the slot clock. It does not participate in consensus.

## Build and Test

```bash
go build ./...       # build
go test ./...        # run all tests
go run .             # run with default flags
```

Go 1.25+ required. No Makefile — use standard Go tooling.

## Project Structure

```
main.go              # Entry point, flag parsing, orchestration
node/
  host.go            # libp2p host creation (TCP/QUIC, noise, yamux+mplex)
  gossipsub.go       # Gossipsub setup with eth2-specific parameters
  attestation.go     # Attestation subscription and latency measurement
discovery/
  discovery.go       # discv5 peer discovery, fork digest filtering, connection loop
rpc/
  rpc.go             # Eth2 RPC handlers (status, ping, metadata, goodbye)
metrics/
  metrics.go         # Prometheus metric definitions
analysis/
  plot_attestations.py  # Post-run latency visualization
```

## Key Concepts

- **Slot timing**: Eth2 slots are 12s. Attestations expected at slot_start + 4s. Latency = receive_time - (slot_start + 4s).
- **Observer mode**: `--gossip-d 10000` prevents active mesh participation.
- **Fork digest**: Used to filter peers on the correct Ethereum fork.
- **Attestation subnets**: 64 subnets; topics follow the pattern `"/eth2/<fork_digest>/beacon_attestation_<subnet>/ssz_snappy"`.
- **Mainnet genesis**: Hardcoded as Unix timestamp 1606824023.

## Dependencies

Key direct dependencies:
- `go-libp2p` — P2P networking
- `go-libp2p-pubsub` — gossipsub (uses local replace directive to `../go-libp2p-pubsub`)
- `prysm/v7` (OffchainLabs fork) — eth2 configs, SSZ encoding, protocol specs
- `go-ethereum` — discv5 discovery, crypto
- `prometheus/client_golang` — metrics

## Conventions

- All packages have unit tests in `_test.go` files.
- Prysm's SSZ/snappy encoding is used for all RPC messages.
- The `metrics` package defines all Prometheus metrics centrally.
- Node keys are secp256k1 ECDSA, hex-encoded raw bytes, compatible with Prysm's format.
- Graceful shutdown via SIGINT/SIGTERM with context cancellation.
- Run `go vet ./...` to check for correctness issues before submitting changes.
- Run `go fix -fix modernize ./...` to apply modern Go idioms (e.g. range-over-int, slices package).
