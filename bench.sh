#!/bin/sh

# REAME
# make bench
# nohup ./bench.sh &
# tail -f nohup.out
# REAME end

export IPFS_GATEWAY="https://proof-parameters.s3.cn-south-1.jdcloud-oss.com/ipfs/"

export FIL_PROOFS_USE_GPU_COLUMN_BUILDER=1 # give us gpu precommit2, https://filecoinproject.slack.com/archives/CEGB67XJ8/p1588805545137700
export FIL_PROOFS_MAXIMIZE_CACHING=0  # open cache for 32GB or 64GB

size=536870912
#size=2048
RUST_LOG=info RUST_BACKTRACE=1 ./bench sealing --storage-dir=/data/cache/.lotus-bench --sector-size=$size --skip-unseal=true --parallel=1
#RUST_LOG=info RUST_BACKTRACE=1 ./bench sealing --storage-dir=/data/cache/.lotus-bench --sector-size=$size --parallel=1

#size=34359738368 # 32GB
#RUST_LOG=info RUST_BACKTRACE=1 ./bench sealing --storage-dir=/data/cache/.lotus-bench --sector-size=$size
