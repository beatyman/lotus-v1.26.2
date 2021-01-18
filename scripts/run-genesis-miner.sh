#!/bin/sh

export RUST_LOG=info
export RUST_BACKTRACE=1

./lotus-miner --repo=/data/lotus/dev/.ldt0111 --miner-repo=/data/lotus/dev/.mdt0111 run --nosync
