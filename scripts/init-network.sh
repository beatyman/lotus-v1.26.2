#!/usr/bin/env bash

set -xeo

NUM_SECTORS=2
SECTOR_SIZE=2048


sdt0111=/data/lotus/dev/.sdt0111 # $(mktemp -d)

staging=/data/lotus/dev/.staging # $(mktemp -d)
rm -rf $sdt0111 && mkdir -p $sdt0111
rm -rf $staging && mkdir -p $staging

make debug
make lotus-shed
make fountain

./lotus-seed genesis new "${staging}/genesis.json"

./lotus-seed --sector-dir="${sdt0111}" pre-seal --sector-offset=0 --sector-size=${SECTOR_SIZE} --num-sectors=${NUM_SECTORS}

./lotus-seed genesis add-miner "${staging}/genesis.json" "${sdt0111}/pre-seal-t01000.json"


ldt0111=/data/lotus/dev/.ldt0111 # $(mktemp -d)
rm -rf $ldt0111 && mkdir -p $ldt0111

lotus_path=$ldt0111
./lotus --repo="${lotus_path}" daemon --lotus-make-genesis="${staging}/devnet.car" --import-key="${sdt0111}/pre-seal-t01000.key" --genesis-template="${staging}/genesis.json" --bootstrap=false &
lpid=$!

sleep 3

kill "$lpid"

wait

cp "${staging}/devnet.car" build/genesis/devnet.car
cp "${staging}/devnet.car" scripts/devnet.car

make debug
git checkout build

./lotus --repo="${ldt0111}" daemon --api "3000$i" --bootstrap=false &
sleep 10
# make the wallet address to default, so it can send by ${ldlist[0]}
./lotus --repo="${ldt0111}" wallet set-default $(./lotus --repo="${ldt0111}" wallet list)

mdt0111=/data/lotus/dev/.mdt0111 # $(mktemp -d)
rm -rf $mdt0111 && mkdir -p $mdt0111

# link the pre-seal data to repo
mkdir -p ${mdt0111}/cache
mkdir -p ${mdt0111}/sealed
mkdir -p ${mdt0111}/unsealed
for sector in `ls ${sdt0111}/cache`
do
    ln -s ${sdt0111}/cache/$sector ${mdt0111}/cache/$sector
done
for sector in `ls ${sdt0111}/sealed`
do
    ln -s ${sdt0111}/sealed/$sector ${mdt0111}/sealed/$sector
done
for sector in `ls ${sdt0111}/unsealed`
do
    ln -s ${sdt0111}/unsealed/$sector ${mdt0111}/unsealed/$sector
done

env LOTUS_PATH="${ldt0111}" LOTUS_STORAGE_PATH="${mdt0111}" ./lotus-storage-miner init --genesis-miner --actor=t01000 --pre-sealed-sectors="${sdt0111}" --pre-sealed-metadata="${sdt0111}/pre-seal-t01000.json" --nosync=true --sector-size="${SECTOR_SIZE}" || true
env LOTUS_PATH="${ldt0111}" LOTUS_STORAGE_PATH="${mdt0111}" ./lotus-storage-miner run --nosync &

wait

