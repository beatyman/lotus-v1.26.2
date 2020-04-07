#!/bin/sh


# nohup ./scripts/init-network.sh >>boostrap.log 2>&1 & # do this at first

ssh-copy-id root@127.0.0.1
./scripts/setup-host.sh root@127.0.0.1
./scripts/deploy-node.sh root@127.0.0.1
./scripts/deploy-bootstrapper.sh root@127.0.0.1
sleep 10
nohup ./fountain run --front=0.0.0.0:7777 --from=$(lotus wallet default) >fountain.log 2>&1 &
