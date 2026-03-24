#!/bin/bash
RUN=crawl1
mkdir -p /storage/beaconnode/$RUN
go run . \
  --tcp-port 12456 \
  --quic-port 12457 \
  --discv5-port 12458 \
  --discv4-port 12459 \
  --metrics-addr :12460 \
  --key-file /storage/beaconnode/crawler.key \
  --log-level info \
  --crawl /storage/beaconnode/$RUN/peers.enr \
  > /storage/beaconnode/$RUN/run.log 2>&1
