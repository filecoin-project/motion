#!/usr/bin/env bash

echo "Wait until motion is running"
for i in {1..10}
do
  docker compose -f ./devnet/docker-compose.yaml ps --services --filter "status=running" | grep motion && break || sleep 5
done