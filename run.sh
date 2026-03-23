#!/bin/bash
RUN=run4
mkdir -p /storage/beaconnode/$RUN
go run . \
  --tcp-port 13456 \
  --quic-port 13457 \
  --disc-port 13458 \
  --subnets 0 \
  --metrics-addr :9893 \
  --log-file-path /storage/beaconnode/$RUN/attestations.log \
  --key-file /storage/beaconnode/node.key \
  --log-level info \
  > /storage/beaconnode/$RUN/run.log 2>&1
