#!/bin/bash
RUN=run12
mkdir -p /storage/beaconnode/$RUN
go run . \
  --tcp-port 13456 \
  --quic-port 13457 \
  --discv5-port 13458 \
  --discv4-port 13459 \
  --subnets 0 \
  --metrics-addr :9893 \
  --log-file-path /storage/beaconnode/$RUN/attestations.log \
  --key-file /storage/beaconnode/node.key \
  --log-level info \
  --quic-only \
  --gossip-d 1000 \
  --disable-ihave \
  --bootstrap-file /storage/beaconnode/crawl3/peers.enr \
  > /storage/beaconnode/$RUN/run.log 2>&1
