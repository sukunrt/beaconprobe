# beaconprobe

A passive Ethereum beacon chain observer that joins the p2p network, subscribes to attestation subnet gossipsub topics, and measures attestation arrival latency relative to the slot clock.

## Usage

```
beaconprobe [flags]
```

## Flags

| Flag | Default | Description |
|------|---------|-------------|
| `--tcp-port` | `9020` | libp2p TCP listen port |
| `--quic-port` | `9021` | libp2p QUIC listen port |
| `--disc-port` | `9022` | discv5 UDP listen port |
| `--subnets` | `0,1,2,3` | Comma-separated attestation subnet IDs to subscribe to |
| `--metrics-addr` | `:9090` | Prometheus metrics listen address |
| `--log-level` | `info` | Log level (`debug`, `info`, `warn`, `error`) |
| `--log-file-path` | | File path to log attestation arrival times |
| `--gossip-d` | `8` | Gossipsub mesh degree. Set high (e.g. `10000`) for observer mode |
