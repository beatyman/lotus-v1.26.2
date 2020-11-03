# 接入校准网

用于业务功能测试

假定有以下五台机器(存储节点略)

##机器1，编译及链备份节点(10.1.1.1)

构建出编译环境，参考[./lotus.md](./lotus.md)

``` shell
cd ~/go/src/github.com/filecoin-project/lotus
./install.sh calibration

cd ~/hlm-miner
. env.sh
hlmd ctl stop all
rm -rf /data/sdb/lotus-user-1/.lotus # 注意df -h确认不是生产环境数据
hlmd ctl start lotus-daemon-1
```

##机器2，启动校验网的链节点(10.1.1.2)
```shell
# 若是新安装，从编译机器完整hlm-miner拷贝过来，即可。

# 若已安装过，执行以下apps更新
cd ~/hlm-miner
. env.sh
hlmd ctl stop all
cd script/lotus/lotus-user
. env/1.sh
. env/lotus.sh
./rsync-apps.sh root@10.1.1.1

# 启动链
hlmd ctl stop lotus-daemon-1
rm -rf /data/sdb/lotus-user-1/.lotus # 注意df -h确认不是生产环境数据
hlmd ctl start lotus-daemon-1

# 若备份链已完成同步，可从备份节点中复制数据过来
hlmd ctl stop lotus-daemon-1 # 需等新链的同步已开始，以便.lotus数据库已初始化
rsync -Pat root@10.1.1.1:/data/sdb/lotus-user-1/.lotus/datastore/ /data/sdb/lotus-user-1/.lotus/datastore/ 
hlmd ctl start lotus-daemon-1
```

# 机器3，启动miner节点(10.1.1.3)
```shell
# 若是新安装，从编译机器完整hlm-miner拷贝过来，即可。

# 若已安装过，执行以下apps更新
cd ~/hlm-miner
. env.sh
hlmd ctl stop all
cd script/lotus/lotus-user
. env/1.sh
. env/lotus.sh
./rsync-apps.sh root@10.1.1.1

# 初始化miner
hlmd ctl stop lotus-user-1
rm -rf /data/sdb/lotus-user-1/.lotus* # 注意df -h确认不是生产环境数据
mkdir -p /data/sdb/lotus-user-1/.lotus
mkdir -p /data/sdb/lotus-user-1/.lotusstorage
root@10.1.1.2:/data/sdb/lotus-user-1/.lotus/api /data/sdb/lotus-user-1/.lotus/
root@10.1.1.2:/data/sdb/lotus-user-1/.lotus/token /data/sdb/lotus-user-1/.lotus/
./lotus.sh wallet new bls
curl "https://faucet.calibration.fildev.network/send?address=$wallet" # 需将$wallet换成实际钱包地址
# 获取到钱后(./lotus.sh wallet list), 执行
./miner.sh init --sector-size=32GiB

# 运行miner
hlmd ctl start lotus-user-1

# 导入存储节点
./miner.sh hlm-storage add xxx

```

# 机器4，运行worker
```shell
# 若是新安装，从编译机器完整hlm-miner拷贝过来，即可。

# 若已安装过，执行以下apps更新
cd ~/hlm-miner
. env.sh
hlmd ctl stop all
cd script/lotus/lotus-user
. env/1.sh
. env/lotus.sh
./rsync-apps.sh root@10.1.1.1

# 复制api密钥
mkdir -p /data/sdb/lotus-user-1/.lotus
mkdir -p /data/sdb/lotus-user-1/.lotusstorage
root@10.1.1.3:/data/sdb/lotus-user-1/.lotus/api /data/sdb/lotus-user-1/.lotus/
root@10.1.1.3:/data/sdb/lotus-user-1/.lotus/token /data/sdb/lotus-user-1/.lotus/
root@10.1.1.3:/data/sdb/lotus-user-1/.lotusstorage/api /data/sdb/lotus-user-1/.lotusstorage/
root@10.1.1.3:/data/sdb/lotus-user-1/.lotusstorage/token /data/sdb/lotus-user-1/.lotusstorage/


# 若之前已部署过tpl的模板，略过此步
cd ~/hlm-miner/etc/hlmd/apps/
cp ./tpl/lotus-worker-1t.dat .
hlmd ctl reload lotus-worker-1t 

hlmd ctl start lotus-worker-1t
```

