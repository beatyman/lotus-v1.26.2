#!/bin/sh

echo "make "$1
case $1 in
    "debug")
        cp -f scripts/bootstrappers.pi build/bootstrap/
        cp -f scripts/devnet.car build/genesis/
        make debug
        git checkout build
    ;;
    *)
        make $1
    ;;
esac

cp -rf lotus $HOME/hlm-miner/apps/lotus/
cp -rf lotus-storage-miner $HOME/hlm-miner/apps/lotus/
cp -rf lotus-seal-worker $HOME/hlm-miner/apps/lotus/

