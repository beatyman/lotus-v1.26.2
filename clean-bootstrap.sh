#!/bin/sh
sudo systemctl stop lotus-fountain &
sudo systemctl stop lotus-daemon &
sudo systemctl stop lotus-genesis-miner &
sudo systemctl stop lotus-genesis-daemon &
wait
sudo rm -rf /data/lotus/dev
sudo rm -rf /root/.lotus
sudo ps axu|grep lotus
