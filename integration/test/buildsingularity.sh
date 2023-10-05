#!/usr/bin/env bash

if [ -n "$SINGULARITY_LOCAL_DOCKERFILE" ]
then
  echo "Building singularity from local source"
  source ./motionlarity/.env
  docker build -t ghcr.io/data-preservation-programs/singularity${SINGULARITY_REF} ${SINGULARITY_LOCAL_DOCKERFILE}
fi