#!/bin/sh

export IPFS_GATEWAY="https://proof-parameters.s3.cn-south-1.jdcloud-oss.com/ipfs/"

repodir=/data/sdb/lotus-user-1/.lotus
storagerepodir=/data/sdb/lotus-user-1/.lotusstorage

mkdir -p $repodir
mkdir -p $storagerepodir

../../lotus-storage-miner --repo=$repodir --storagerepo=$storagerepodir $@
