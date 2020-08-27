#!/bin/sh

RUST_LOG=info RUST_BACKTRACE=1 ./lotus-hlm-worker --repo=/data/sdb/lotus-user-1/.lotus --miner-repo=/data/sdb/lotus-user-1/.lotusstorage test wdpost --index=1
