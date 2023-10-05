#!/usr/bin/env bash

echo "setup boost pricing"
for i in {1..10}
do
  curl -X POST -d '{"operationName":"AppStorageAskUpdateMutation","variables":{"update":{"Price":"0", "VerifiedPrice": 0}},"query":"mutation AppStorageAskUpdateMutation($update: StorageAskUpdate!) {\n  storageAskUpdate(update: $update)\n}\n"}' http://localhost:8080/graphql/query && break || sleep 5
done