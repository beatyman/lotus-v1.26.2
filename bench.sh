#!/bin/sh

export IPFS_GATEWAY="https://proof-parameters.s3.cn-south-1.jdcloud-oss.com/ipfs/"

#make bench

size=536870912
RUST_LOG=info RUST_BACKTRACE=1 TMPDIR=/data/cache/tmp ./bench sealing --storage-dir=/data/cache/.lotus-bench --sector-size=$size --no-gpu

#size=34359738368 # 32GB
#FIL_PROOFS_MAXIMIZE_CACHING=1 ./bench sealing --storage-dir=/data/cache/.lotus-bench --sector-size=$size --no-gpu
