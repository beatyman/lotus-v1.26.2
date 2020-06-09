#!/bin/sh

size=536870912
#size=2048
#RUST_LOG=info RUST_BACKTRACE=1 ./lotus-bench sealing --storage-dir=/data/cache/.lotus-bench --sector-size=$size --skip-unseal=true --parallel=2
RUST_LOG=info RUST_BACKTRACE=1 ./lotus-bench sealing --storage-dir=/data/cache/.lotus-bench --sector-size=$size --parallel=1

#size=34359738368 # 32GB
#FIL_PROOFS_MAXIMIZE_CACHING=1 RUST_LOG=info RUST_BACKTRACE=1 ./lotus-bench sealing --storage-dir=/data/cache/.lotus-bench --sector-size=$size 
