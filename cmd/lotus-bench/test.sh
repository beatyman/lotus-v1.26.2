#!/bin/sh

size=536870912
#size=34359738368 # 32GB
./lotus-bench --storage-dir=/data/cache/.lotus-bench --sector-size=$size
