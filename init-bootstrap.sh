#!/bin/sh

nohup ./scripts/init-network.sh debug >>bootstrap-init.log 2>&1 &

echo "using 'tail -f bootstrap-init.log' to see the logs."
