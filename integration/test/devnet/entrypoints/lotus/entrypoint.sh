#!/usr/bin/env bash
set -e

export LOTUS_SKIP_GENESIS_CHECK=${LOTUS_SKIP_GENESIS_CHECK:-_yes_}
export GENESIS_PATH=${GENESIS_PATH:-/var/lib/genesis}
export SECTOR_SIZE=${SECTOR_SIZE:-8388608}
export LOTUS_FEVM_ENABLEETHRPC=${LOTUS_FEVM_ENABLEETHRPC:-true}
export LOTUS_PATH=${LOTUS_PATH:-}


if [ ! -f $LOTUS_PATH/.init.params ]; then
	echo Initializing fetch params ...
	lotus fetch-params $SECTOR_SIZE
	touch $LOTUS_PATH/.init.params
	echo Done
fi

if [ ! -f $LOTUS_PATH/.init.genesis ]; then
	echo Initializing pre seal ...
	lotus-seed --sector-dir $GENESIS_PATH pre-seal --sector-size $SECTOR_SIZE --num-sectors 1
	echo Initializing genesis ...
	lotus-seed --sector-dir $GENESIS_PATH genesis new $LOTUS_PATH/localnet.json
	echo Initializing address ...
	lotus-seed --sector-dir $GENESIS_PATH genesis add-miner $LOTUS_PATH/localnet.json $GENESIS_PATH/pre-seal-t01000.json
	touch $LOTUS_PATH/.init.genesis
	echo Done
fi

echo Starting lotus deamon ...
exec lotus daemon --lotus-make-genesis=$LOTUS_PATH/devgen.car --genesis-template=$LOTUS_PATH/localnet.json --bootstrap=false
