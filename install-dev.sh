#!/bin/sh

FFI_BUILD_FROM_SOURCE=1 make debug

cp -rf lotus $HOME/hlm-miner/apps/lotus/lotus-dev
cp -rf lotus-storage-miner $HOME/hlm-miner/apps/lotus/lotus-storage-miner-dev
cp -rf lotus-seal-worker $HOME/hlm-miner/apps/lotus/
cp -rf lotus-seed $HOME/hlm-miner/apps/lotus/lotus-seed
