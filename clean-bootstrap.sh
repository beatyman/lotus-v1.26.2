#!/bin/sh
sudo systemctl stop lotus-fountain &
sudo systemctl stop lotus-daemon &
sudo systemctl stop lotus-genesis-miner &
sudo systemctl stop lotus-genesis-daemon &
wait
sudo systemctl disable lotus-fountain &
sudo systemctl disable lotus-daemon &
sudo systemctl disable lotus-genesis-miner &
sudo systemctl disable lotus-genesis-daemon &
wait

killall -9 lotus
killall -9 lotus-miner
killall -9 lotus-fountain
killall -9 lotus-seed
sudo rm -rf /tmp/bellman*.lock
sudo rm -rf /data/lotus/dev
sudo rm -rf /root/.lotus
sudo ps axu|grep lotus
