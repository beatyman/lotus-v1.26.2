#!/bin/sh

nohup ./scripts/init-network.sh hlm >>bootstrap-init.log 2>&1 & # do this at first
