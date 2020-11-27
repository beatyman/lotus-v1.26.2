#!/bin/sh

# REAME
# make bench
# nohup ./bench.sh &
# tail -f nohup.out
# REAME end

export IPFS_GATEWAY="https://proof-parameters.s3.cn-south-1.jdcloud-oss.com/ipfs/"

# Note that FIL_PROOFS_USE_GPU_TREE_BUILDER=1 is for tree_r_last building and FIL_PROOFS_USE_GPU_COLUMN_BUILDER=1 is for tree_c.  
# So be sure to use both if you want both built on the GPU
export FIL_PROOFS_USE_GPU_COLUMN_BUILDER=0
export FIL_PROOFS_USE_GPU_TREE_BUILDER=0 
export FIL_PROOFS_MAXIMIZE_CACHING=1  # open cache for 32GB or 64GB
export FIL_PROOFS_USE_MULTICORE_SDR=1
export BELLMAN_NO_GPU=1

# checking gpu
gpu=""
type nvidia-smi
if [ $? -eq 0 ]; then
    gpu=$(nvidia-smi -L|grep "GPU")
fi
if [ ! -z "$gpu" ]; then
    FIL_PROOFS_USE_GPU_COLUMN_BUILDER=1
    FIL_PROOFS_USE_GPU_TREE_BUILDER=1
    BELLMAN_NO_GPU=0
fi

#size=34359738368 # 32GB
#size=536870912 # 512MB
size=2048
#RUST_LOG=info RUST_BACKTRACE=1 ./lotus-bench sealing --storage-dir=/data/cache/.lotus-bench --sector-size=$size &
RUST_LOG=info RUST_BACKTRACE=1 ./lotus-bench p-run --storage-dir=/data/cache/.lotus-bench --sector-size=$size \
    --max-tasks=2 \
    --parallel-addpiece=2 \
    --parallel-precommit1=2 \
    --parallel-precommit2=1 \
    --parallel-commit1=1 \
    --parallel-commit2=1 &
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

