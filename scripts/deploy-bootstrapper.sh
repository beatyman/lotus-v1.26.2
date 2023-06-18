#!/usr/bin/env bash

log() {
  echo -e "\e[33m$1\e[39m"
}

log "> Deploying bootstrap node"
log "Stopping lotus daemon"

sudo systemctl stop lotus-fountain &
sudo systemctl stop lotus-daemon &
sudo systemctl stop lotus-genesis-miner &
sudo systemctl stop lotus-genesis-daemon &
wait 

log 'Initializing repo'

sudo mkdir -p /data/lotus/dev/.lotus
sudo mkdir -p /var/log/lotus
if sudo test "-f /root/.lotus"; then
    sudo rm -rf /root/.lotus
    sudo ln -s /data/lotus/dev/.lotus /root/.lotus
fi

sudo cp -f lotus /usr/local/bin
sudo cp -f lotus-miner /usr/local/bin
sudo cp -f lotus-fountain /usr/local/bin

sudo cp -f scripts/lotus-genesis-daemon.service /etc/systemd/system/lotus-genesis-daemon.service
sudo cp -f scripts/lotus-genesis-miner.service /etc/systemd/system/lotus-genesis-miner.service
sudo cp -f scripts/lotus-daemon.service /etc/systemd/system/lotus-daemon.service
sudo cp -f scripts/lotus-fountain.service /etc/systemd/system/lotus-fountain.service

sudo systemctl daemon-reload

# start the genesis
sudo systemctl enable lotus-genesis-daemon
sudo systemctl start lotus-genesis-daemon
sleep 30
sudo systemctl enable lotus-genesis-miner
sudo systemctl start lotus-genesis-miner

sudo systemctl enable lotus-daemon
sudo systemctl start lotus-daemon

sudo cp scripts/bootstrap.toml /root/.lotus/config.toml
#sudo bash -c "echo -e '[Metrics]\nNickname=\"Boot-bootstrap\"' >> /root/.lotus/config.toml"
sudo systemctl restart lotus-daemon

sleep 30

log 'Extracting addr info'
sudo lotus --repo=/data/lotus/dev/.lotus net listen > scripts/bootstrappers.pi
cat scripts/bootstrappers.pi
log 'Connect to t0111'
genesisAddr=$(sudo lotus --repo=/data/lotus/dev/.ldt0111 net listen|grep "127.0.0.1")
sudo lotus --repo=/data/lotus/dev/.lotus net connect $genesisAddr

hasDefaultWalletAddr=$(sudo lotus wallet default)
if [ $? -ne 0 ]; then
    log 'Get fil from t0111'
    walletAddr=$(sudo lotus wallet new --encode=false bls)
    sudo lotus --repo=/data/lotus/dev/.ldt0111 send $walletAddr 90000000
    git checkout build
fi

sudo systemctl enable lotus-fountain
sudo systemctl start lotus-fountain

sudo ps axu|grep "lotus"

echo "fountain log:         tail -f /var/log/lotus/fountain.log"
echo "daemon log: tail -f /var/log/lotus/daemon.log"
echo "genesis daemon log:   tail -f /var/log/lotus/genesis-daemon.log"
echo "genesis miner log:    tail -f /var/log/lotus/genesis-miner.log"

