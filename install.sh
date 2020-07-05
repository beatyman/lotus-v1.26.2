#!/bin/sh

install_path=$HOME/hlm-miner/apps/lotus;
if [ ! -z "$FILECOIN_BIN"]; then
    install_path=$FILECOIN_BIN
fi

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

cp -rf lotus $install_path
cp -rf lotus-storage-miner $install_path
cp -rf lotus-seal-worker $install_path
cp -rf lotus-chain-watch $install_path
