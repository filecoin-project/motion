#!/usr/bin/env bash

cd integration/test/devnet
# Setup Lotus API token
export `docker compose exec lotus lotus auth api-info --perm=admin`
IFS=: read -r token path <<< "${FULLNODE_API_INFO}"
# Setup Motion Wallet
MOTION_WALLET_ADDR=`docker compose exec lotus lotus wallet new`
MOTION_WALLET_KEY=`docker compose exec lotus lotus wallet export ${MOTION_WALLET_ADDR}`
LOTUS_WALLET_DEFAULT_ADDR=`docker compose exec lotus lotus wallet default`
docker compose exec lotus lotus send --from=${LOTUS_WALLET_DEFAULT_ADDR} ${MOTION_WALLET_ADDR} 10
echo "LOTUS_TOKEN=${token}" >> $1
echo "MOTION_WALLET_ADDR=${MOTION_WALLET_ADDR}" >> $1
echo "MOTION_WALLET_KEY=${MOTION_WALLET_KEY}" >> $1
echo "MOTION_STORAGE_PROVIDERS=t01000" >> $1
echo "MOTION_API_ENDPOINT=http://localhost:40080" >> $1
echo "SINGULARITY_API_ENDPOINT=http://localhost:9091" >> $1