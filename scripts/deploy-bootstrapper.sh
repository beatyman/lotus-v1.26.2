#!/usr/bin/env bash

log() {
  echo -e "\e[33m$1\e[39m"
}

log "> Deploying bootstrap node"
log "Stopping lotus daemon"

sudo systemctl stop lotus-fountain &
sudo systemctl stop lotus-bootstrap-daemon &
sudo systemctl stop lotus-genesis-miner &
sudo systemctl stop lotus-genesis-daemon &
wait 

log 'Initializing repo'

sudo mkdir -p /data/lotus/dev/.lotus
sudo mkdir -p /var/log/lotus
sudo rm -rf /root/.lotus
sudo ln -s /data/lotus/dev/.lotus /root/.lotus

sudo cp -f lotus /usr/local/bin
sudo cp -f lotus-miner /usr/local/bin
sudo cp -f lotus-fountain /usr/local/bin

sudo cp -f scripts/lotus-genesis-daemon.service /etc/systemd/system/lotus-genesis-daemon.service
sudo cp -f scripts/lotus-genesis-miner.service /etc/systemd/system/lotus-genesis-miner.service
sudo cp -f scripts/lotus-bootstrap-daemon.service /etc/systemd/system/lotus-bootstrap-daemon.service
sudo cp -f scripts/lotus-fountain.service /etc/systemd/system/lotus-fountain.service

sudo systemctl daemon-reload

# start the genesis
sudo systemctl enable lotus-genesis-daemon
sudo systemctl start lotus-genesis-daemon
sleep 30
sudo systemctl enable lotus-genesis-miner
sudo systemctl start lotus-genesis-miner

sudo systemctl enable lotus-bootstrap-daemon
sudo systemctl start lotus-bootstrap-daemon

sudo cp scripts/bootstrap.toml /root/.lotus/config.toml
sudo bash -c "echo -e '[Metrics]\nNickname=\"Boot-bootstrap\"' >> /root/.lotus/config.toml"
sudo systemctl restart lotus-bootstrap-daemon

sleep 30

log 'Extracting addr info'
sudo lotus --repo=/data/lotus/dev/.lotus net listen > scripts/bootstrappers.pi

log 'Connect to t0111'
genesisAddr=$(sudo lotus --repo=/data/lotus/dev/.ldt0111 net listen|grep "127.0.0.1")
sudo lotus --repo=/data/lotus/dev/.lotus net connect $genesisAddr

log 'Get fil from t0111'
walletAddr=$(sudo lotus wallet new bls)
sudo lotus --repo=/data/lotus/dev/.ldt0111 send $walletAddr 90000000
git checkout build

sudo systemctl enable lotus-fountain
sudo systemctl start lotus-fountain

echo "genesis daemon log:   tail -f /var/log/lotus/genesis-daemon.log"
echo "genesis miner log:    tail -f /var/log/lotus/genesis-miner.log"
echo "bootstrap daemon log: tail -f /var/log/lotus/bootstrap-daemon.log"
echo "fountain log:         tail -f /var/log/lotus/fountain.log"

