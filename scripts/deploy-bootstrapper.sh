#!/usr/bin/env bash

log() {
  echo -e "\e[33m$1\e[39m"
}

log "> Deploying bootstrap node"
log "Stopping lotus daemon"

sudo systemctl stop lotus-daemon &
sudo systemctl stop lotus-miner &
wait 

log 'Initializing repo'

mkdir -p /data/lotus/dev/.lotus
ln -s /data/lotus/dev/.lotus $HOME/.lotus
sudo cp -f lotus /usr/local/bin
sudo cp -f lotus-miner /usr/local/bin
sudo cp -f scripts/lotus-daemon.service /etc/systemd/system/lotus-daemon.service
sudo mkdir -p /var/log/lotus
sudo systemctl daemon-reload

sudo systemctl start lotus-daemon
cp scripts/bootstrap.toml $HOME/.lotus/config.toml
sudo echo -e '[Metrics]\nNickname="Boot-bootstrap"' >> $HOME/.lotus/config.toml
sudo systemctl restart lotus-daemon

sleep 30

log 'Extracting addr info'
lotus net listen > scripts/bootstrappers.pi

log 'Connect to t0111'
sudo lotus net connect $(lotus --repo=/data/lotus/dev/.ldt0111 net listen)

log 'Get fil from t0111'
lotus wallet new bls
lotus --repo=/data/lotus/dev/.ldt0111 send $(lotus wallet default) 90000000
git checkout build

