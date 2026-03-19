#!/bin/bash
mkdir -p /storage/beaconnode/run1
go run . \
  --tcp-port 13456 \
  --quic-port 13457 \
  --disc-port 13458 \
  --subnets 0 \
  --metrics-addr :9893 \
  --log-file-path /storage/beaconnode/run1/attestations.log \
  --key-file /storage/beaconnode/run1/node.key \
  --log-level info \
  > /storage/beaconnode/run1/run.log 2>&1
