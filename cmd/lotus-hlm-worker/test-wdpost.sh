#!/bin/sh


RUST_LOG=info RUST_BACKTRACE=1 ./lotus-hlm-worker --repo=/data/sdb/lotus-user-1/.lotus --miner-repo=/data/sdb/lotus-user-1/.lotusstorage test wdpost --index=1 --mount=false --do-wdpost=true &
pid=$!

# set ulimit for process
nropen=$(cat /proc/sys/fs/nr_open)
echo "max nofile limit:"$nropen
echo "current nofile of $pid limit:"$(cat /proc/$pid/limits|grep "open files")
prlimit -p $pid --nofile=$nropen
if [ $? -eq 0 ]; then
    echo "new nofile of $pid limit:"$(cat /proc/$pid/limits|grep "open files")
else
    echo "set prlimit failed, command:prlimit -p $pid --nofile=$nropen"
    exit 0
fi

wait $pid

