#!/bin/sh

install_path=$HOME/hlm-miner/apps/lotus;
if [ ! -z "$FILECOIN_BIN" ]; then
    install_path=$FILECOIN_BIN
fi
mkdir -p $install_path

# env for build
export RUSTFLAGS="-C target-cpu=native -g" 
export CGO_CFLAGS="-D__BLST_PORTABLE__"
export FFI_BUILD_FROM_SOURCE=1

echo "make "$1
case $1 in
    "debug")
        cp -f scripts/bootstrappers.pi build/bootstrap/bootstrappers.pi
        cp -f scripts/devnet.car build/genesis/devnet.car
        make debug
        git checkout build
    ;;
    "hlm")
        cp -f scripts/bootstrappers.pi build/bootstrap/bootstrappers.pi
        cp -f scripts/hlmnet.car build/genesis/devnet.car
        make hlm
        git checkout build
    ;;
    *)
        make $1
    ;;
esac

echo "copy bin to "$install_path
cp -rf lotus $install_path
cp -rf lotus-miner $install_path
cp -rf lotus-worker $install_path
cp -rf lotus-chain-watch $install_path
if [ -f ./lotus-bench ]; then
    cp -rf lotus-bench $install_path
fi
