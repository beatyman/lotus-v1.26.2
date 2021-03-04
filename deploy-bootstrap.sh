#!/bin/sh


# nohup ./scripts/init-network.sh >>boostrap.log 2>&1 & # do this at first

./scripts/deploy-bootstrapper.sh
sleep 10
nohup ./lotus-fountain run --front=0.0.0.0:7777 --from=$(lotus wallet default) --amount=10000 >lotus-fountain.log 2>&1 &
