#!/bin/sh

if [ -z "$1" ];then
    cp -f scripts/bootstrappers.pi build/bootstrap/
    cp -f scripts/devnet.car build/genesis/
fi

echo "make "$1

make $1


cp -rf lotus $HOME/hlm-miner/apps/lotus/
cp -rf lotus-storage-miner $HOME/hlm-miner/apps/lotus/
cp -rf lotus-seal-worker $HOME/hlm-miner/apps/lotus/

git checkout build
