#!/bin/sh

install_path=$HOME/hlm-miner/apps/lotus;
if [ ! -z "$FILECOIN_BIN" ]; then
    install_path=$FILECOIN_BIN
fi
mkdir -p $install_path

# set the build path for cert
certfrom="scripts"
if [ ! -z $FILECOIN_CERT ]; then
    certfrom=$FILECOIN_CERT
fi

# env for build
export RUSTFLAGS="-C target-cpu=native -g" 
export CGO_CFLAGS="-D__BLST_PORTABLE__"
export FFI_BUILD_FROM_SOURCE=1

# copy the cert key for build
if [ -f $certfrom/root.key ]; then
    cp -v $certfrom/root.key build/cert/root.key
fi
if [ -f $certfrom/root-old.key ]; then
    cp -v $certfrom/root-old.key build/cert/root-old.key
fi

echo "make "$1
case $1 in
    "debug")
        cp -v scripts/bootstrappers.pi build/bootstrap/devnet.pi
        cp -v scripts/devnet.car build/genesis/devnet.car
        make debug
    ;;
    "hlm")
        cp -v scripts/bootstrappers.pi build/bootstrap/devnet.pi
        cp -v scripts/devnet-hlm.car build/genesis/devnet.car
        make hlm
    ;;
    "calibration")
        make calibnet
    ;;
    *)
        make $1
    ;;
esac
# roolback build resource
git checkout build

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
if [ -f ./lotus-bench ]; then
    cp -vrf lotus-bench $install_path
fi
if [ -f ./leveldb-tools ]; then
    cp -vrf leveldb-tools $install_path
fi
if [ -f ./lotus-storage ]; then
    cp -vrf lotus-storage $install_path
fi
if [ -f ./lotus-worker ]; then
    cp -vrf lotus-worker $HOME/hlm-miner/apps/lotus/lotus-worker-wnpost
fi
