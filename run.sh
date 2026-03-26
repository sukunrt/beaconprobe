#!/bin/bash
RUN=run39
mkdir -p /storage/beaconnode/$RUN

cat > /storage/beaconnode/$RUN/config.yaml <<'YAML'
mode: probe
key-dir: /storage/beaconnode
metrics-addr: ":9897"
base-tcp-port: 13472
base-quic-port: 13473
base-discv5-port: 13474
discv4-port: 13475
subnets: [0]
listen-blocks: true
bootstrap-file: /storage/beaconnode/crawl3/peers.enr
log-level: info
instances:
  - name: d8
    gossip-d: 8
    max-peers: 100
    log-file: /storage/beaconnode/$RUN/attestations.log
YAML

# Expand $RUN inside the generated YAML.
sed -i "s|\$RUN|$RUN|g" /storage/beaconnode/$RUN/config.yaml

go run . --config /storage/beaconnode/$RUN/config.yaml \
  > /storage/beaconnode/$RUN/run.log 2>&1
