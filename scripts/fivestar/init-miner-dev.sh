#!/bin/sh

#host="http://120.77.213.165:7776"
host="http://127.0.0.1:7777"
size=2048
#size=536870912 # 512MB
#size=34359738368 # 32G

walletAddr=$(./lotus.sh wallet new bls)
echo $walletAddr
mkresp=$(curl -s $host"/mkminer?sectorSize=$size&address="$walletAddr)

echo $mkresp
f=$(echo $mkresp|awk -F 'a href="/wait.html\\?f=' '{print $2}'|awk -F '&amp;m=' '{print $1}')
if [ -z "$f" ]; then
    echo "f not found"
    exit
fi

curl -s $host"/msgwait?cid="$f
minerAddrJs=$(curl -s $host"/msgwaitaddr?cid="$f)
minerAddr=$(echo $minerAddrJs|awk -F '"addr": "' '{print $2}'|awk -F '"}' '{print $1}')

../../lotus.sh wallet export $walletAddr>$minerAddr-$walletAddr.dat
echo lotus-storage-miner init --actor=$minerAddr --owner=$walletAddr
