
# 构建内部大型压测节点
因外网已无法提供大规模的测试环境，需要自行搭建压测节点


假定有以下五台机器(存储节点略)

##机器1，编译及创世节点(10.1.1.1, 需要256GB内存及GPU)

构建出编译环境，参考[./lotus.md](./lotus.md)

创建创世节点(实际同开发节点创建，参考[./lotus.md](./lotus.md)，只是参数使用主网参数)

``` shell
# 创世节点
cd ~/go/src/github.com/filecoin-project/lotus
./init-bootstrap-hlm.sh
tail -f nohup.output # 直到稳定30秒输出tipset

./deploy-bootstrap.sh

ps axu|grep lotus # 核对相关进程

# 编译包含创世节点信息
./install.sh hlm # 得到编译包

```
##机器2，启动内部测试网的链节点(10.1.1.2)
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
curl "http://10.1.1.1:7777/send?address=$wallet" # 需将$wallet换成实际钱包地址
# 获取到钱后(./lotus.sh wallet list), 执行
./miner.sh init --sector-size=32GiB

# 运行miner
hlmd ctl start lotus-user-1

# 导入存储节点
./miner.sh hlm-storage add xxx

```

以下步骤同[calibration.md](./calibration.md), 请按该步骤部署其他的节点



