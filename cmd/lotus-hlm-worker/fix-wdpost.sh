#!/bin/sh

sectors="
s-t03176-1475
"

for s in $sectors 
do
    if [ -f /data/sdb/lotus-user-1/.lotusstorage/cache/$s/p_aux ]; then
        mv /data/sdb/lotus-user-1/.lotusstorage/cache/$s/p_aux /data/sdb/lotus-user-1/.lotusstorage/cache/$s/p_aux.bak
        echo "mv /data/sdb/lotus-user-1/.lotusstorage/cache/$s/p_aux"
    fi
done
