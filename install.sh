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
        cp -f scripts/bootstrappers.pi build/bootstrap/devnet.pi
        cp -f scripts/devnet.car build/genesis/devnet.car
        make debug
        git checkout build
    ;;
    "hlm")
        cp -f scripts/bootstrappers.pi build/bootstrap/devnet.pi
        cp -f scripts/devnet-hlm.car build/genesis/devnet.car
        make hlm
        git checkout build
    ;;
    "calibration")
        make calibnet
    ;;
    *)
        make $1
    ;;
esac

echo "copy bin to "$install_path
if [ -f ./etcd ]; then
    cp -vrf etcd $install_path
fi
if [ -f ./etcdctl ]; then
    cp -vrf etcdctl $install_path
fi
if [ -f ./lotus ]; then
    cp -vrf lotus $install_path
fi
if [ -f ./lotus-miner ]; then
    cp -vrf lotus-miner $install_path
fi
if [ -f ./lotus-worker ]; then
    cp -vrf lotus-worker $install_path
fi
if [ -f ./lotus-chain-watch ]; then
    cp -vrf lotus-chain-watch $install_path
fi
if [ -f ./lotus-shed ]; then
    cp -rf lotus-shed $install_path
fi
if [ -f ./leveldb-tools ]; then
    cp -vrf leveldb-tools $install_path
fi
if [ -f ./lotus-bench ]; then
    cp -vrf lotus-bench $install_path
fi
