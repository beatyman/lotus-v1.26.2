#!/bin/sh
systemctl stop lotus-daemon
killall lotus
killall lotus-storage-miner
killall fountain
killall lotus-seed
rm -rf /data/lotus/dev
rm -rf ~/.lotus
