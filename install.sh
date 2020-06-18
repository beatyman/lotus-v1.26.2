#!/bin/sh

echo "make "$1
case $1 in
    "debug")
        cp -f scripts/bootstrappers.pi build/bootstrap/bootstrappers.pi
        cp -f scripts/devnet.car build/genesis/devnet.car
        env RUSTFLAGS="-C target-cpu=native -g" FFI_BUILD_FROM_SOURCE=1 make debug
        git checkout build
    ;;
    "hlm")
        cp -f scripts/bootstrappers.pi build/bootstrap/bootstrappers.pi
        cp -f scripts/hlmnet.car build/genesis/devnet.car
        env RUSTFLAGS="-C target-cpu=native -g" FFI_BUILD_FROM_SOURCE=1 make hlm
        git checkout build
    ;;
    *)
        env RUSTFLAGS="-C target-cpu=native -g" FFI_BUILD_FROM_SOURCE=1 make $1
        #make $1
    ;;
esac

cp -rf lotus $HOME/hlm-miner/apps/lotus/
cp -rf lotus-storage-miner $HOME/hlm-miner/apps/lotus/
cp -rf lotus-seal-worker $HOME/hlm-miner/apps/lotus/
cp -rf lotus-chain-watch $HOME/hlm-miner/apps/lotus/

