#!/bin/bash
RUN=run39
mkdir -p /storage/beaconnode/$RUN
go run . \
  --tcp-port 13472 \
  --quic-port 13473 \
  --discv5-port 13474 \
  --discv4-port 13475 \
  --subnets 0 \
  --metrics-addr :9897 \
  --max-peers 100 \
  --log-file-path /storage/beaconnode/$RUN/attestations.log \
  --key-file /storage/beaconnode/node5.key \
  --log-level info \
  --gossip-d 8 \
  --bootstrap-file /storage/beaconnode/crawl3/peers.enr \
  > /storage/beaconnode/$RUN/run.log 2>&1
