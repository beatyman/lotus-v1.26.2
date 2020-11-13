#!/bin/sh
systemctl stop lotus-daemon
killall -9 lotus
killall -9 lotus-miner
killall -9 lotus-fountain
killall -9 lotus-seed
rm -rf /data/lotus/dev
rm -rf ~/.lotus
ps axu|grep lotus
