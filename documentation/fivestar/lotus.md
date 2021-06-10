# 搭建开发环境

- [开发环境安装](#开发环境安装)
- [国内安装技巧](#国内安装技巧)
- [下载lotus源码](#下载lotus源码)
- [启动2k开发网络](#启动2k开发网络)
    - [搭建创世节点](#搭建创世节点)
    - [搭建存储节点](#搭建存储节点)
    - [运行矿工程序](#运行矿工程序)
- [启用链集群(可选)](#启用链集群(可选))
- [目录规范](#目录规范)
    - [存储节点上的目录](#存储节点上的目录)
    - [链节点目录](#链节点目录)
    - [矿工节点目录](#矿工节点目录)
    - [计算节点目录](#计算节点目录)
- [发布二进制](#发布二进制)
- [附录](#附录)

## 开发环境安装
阅读此文档需要具备linux shell能力，未有此能力前应先熟悉linux的命令, 可参考附录中的ide入门中的linux部分。

```shell
# 安装依赖(需要ubuntu 18.04, sudo账号)
apt-get update
apt-get install aptitude
aptitude install rsync chrony fuse make mesa-opencl-icd ocl-icd-opencl-dev gcc git bzr jq pkg-config curl clang build-essential libhwloc-dev
```

## 国内安装技巧 
参考: https://docs.lotu.sh/en+install-lotus-ubuntu

### 1), 安装go
```shell
sudo su -
cd /usr/local/
wget https://studygolang.com/dl/golang/go1.16.5.linux-amd64.tar.gz # 其他版本请参考https://studygolang.com/dl
#wget https://golang.org/dl/go1.16.5.linux-amd64.tar.gz # 官方源
#scp root@10.1.1.33:/root/rsync/go1.16.5.linux-amd64.tar.gz . #内网复制

tar -xzf go1.16.5.linux-amd64.tar.gz
### 配置/etc/profile环境变量(需要重新登录生效或source /etc/profile)
export GOROOT=/usr/local/go
export GOPROXY="https://goproxy.io,direct"
export GOPRIVATE="github.com/filecoin-fivestar"
export GIT_TERMINAL_PROMPT=1
export PATH=$GOROOT/bin:$PATH:/bin:/usr/local/sbin:/usr/local/bin:/usr/sbin:/usr/bin:/sbin:/bin:/usr/games:/usr/local/games:/snap/bin

export FIL_PROOFS_PARENT_CACHE="/data/cache/filecoin-parents"
export FIL_PROOFS_PARAMETER_CACHE="/data/cache/filecoin-proof-parameters/v28" 

# 仅限开发环境配置, 开启后使官方默认兼容CPU的算法进行计算。
# 或者通过hlmc set-env FIL_PROOFS_GPU_MODE auto 设定, 设定后需重启worker程序, force值为须有GPU。
export FIL_PROOFS_GPU_MODE="auto" # 设定后需要重启系统, force值为须有GPU。
exit # 退出sudo su -
```

### 2)，安装rust
```shell
mkdir ~/.cargo

### 设置国内镜像代理(或设置到~/.profile中, 需要重新登录生效或source ~/.profile))
export RUSTUP_DIST_SERVER=https://mirrors.sjtug.sjtu.edu.cn/rust-static
export RUSTUP_UPDATE_ROOT=https://mirrors.sjtug.sjtu.edu.cn/rust-static/rustup
# 若前面源不可用，换以下源
#export RUSTUP_DIST_SERVER=https://mirrors.ustc.edu.cn/rust-static
#export RUSTUP_UPDATE_ROOT=https://mirrors.ustc.edu.cn/rust-static/rustup

cat > ~/.cargo/config <<EOF
[net]
git-fetch-with-cli = true

[source.crates-io]
registry = "https://github.com/rust-lang/crates.io-index"
# 指定镜像
replace-with = 'sjtu'

# 清华大学
[source.tuna]
registry = "https://mirrors.tuna.tsinghua.edu.cn/git/crates.io-index.git"

# 中国科学技术大学
[source.ustc]
registry = "git://mirrors.ustc.edu.cn/crates.io-index"

# 上海交通大学
[source.sjtu]
registry = "https://mirrors.sjtug.sjtu.edu.cn/git/crates.io-index"

# rustcc社区
[source.rustcc]
registry = "https://code.aliyun.com/rustcc/crates.io-index.git"
EOF

curl --proto '=https' --tlsv1.2 -sSf https://sh.rustup.rs | sh
```

## 下载lotus源码
```shell
mkdir -p $HOME/go/src/github.com/filecoin-project
cd $HOME/go/src/github.com/filecoin-project
# 下载慢时可配置/etc/hosts的github指向来替换
# https://blog.csdn.net/random0708/article/details/106001665/
git clone --origin fivestar https://github.com/filecoin-fivestar/fivestar-lotus.git lotus
cd lotus
git checkout testing # 检出需要的分支
# 编译
make clean
env RUSTFLAGS="-C target-cpu=native -g" CGO_CFLAG="-D__BLST_PORTABLE__" FFI_BUILD_FROM_SOURCE=1 make

# 添加官方代码库
git remote -v
git remote add origin https://github.com/filecoin-project/lotus.git
git fetch origin
```

## 安装部署程序
下载hlm-miner(开源版)程序管理
```shell
cd ~
git clone https://github.com/filecoin-fivestar/hlm-miner.git

mkdir -p ~/go/src/github.com/filecoin-fivestar/
cd ~/go/src/github.com/filecoin-fivestar/
git clone https://github.com/filecoin-fivestar/supd
cd ~/go/src/github.com/filecoin-fivestar/supd/cmd/supervisord
./publish.sh
cp -rf supervisord ~/hlm-miner/bin/hlmd

cd ~/hlm-miner/
git checkout testing # 检出最新代码
. env.sh
./install.sh install # hlmc status # 有状态输出为成功
```

## 启动2k开发网络

### 搭建创世节点
```shell
cd $HOME/go/src/github.com/filecoin-project/lotus
./clean-bootstrap.sh
sudo mkdir -p /data/lotus
sudo chown -R $USER:$USER /data/lotus

./clean-boostrap.sh

ps axu|grep lotus # 确认所有相关进程已关闭
./init-bootstrap.sh
tail -f bootstrap-init.log # 直到'init done', ctrl+c 退出

./deploy-boostrap.sh # 部署守护进程, 人工等上一步完成
# 遇错时./clean-boostrap.sh重新开始

# 重启创世节点
sudo systemctl restart lotus-genesis-daemon # 重启创世节点链
sudo systemctl restart lotus-genesis-miner # 重启创世节点矿工

sudo systemctl restart lotus-daemon # 重启对外启动节点
sudo lotus --repo=/data/lotus/dev/.ldt0111 net listen
sudo lotus net connect [上述创世节点的地址] # net connect后外对链节点才会从创世链中取到链同步

sudo systemctl restart lotus-fountain # 重新水龙头
或
ps axu|grep lotus
kill -9 xxxx # 相关进程pid

sudo lotus sync status # 查看bootstrap节点的链状态
```

### 搭建存储节点
重置存储节点删除所有配置文件后重新初始化即可。
```shell
sudo rm -rf ~/hlm-miner/var/lotus-storage-0/auth.dat # 重置原有的api密钥
sudo mkdir -p /data/zfs # 可不挂载、或挂载zfs，或挂载单盘，或某个支持mount的数据池
cp ~/hlm-miner/etc/hlmd/apps/tpl/lotus-storage-0.ini ~/hlm-miner/etc/hlmd/apps/ # 若原已存在，不需再复制过来
hlmc reload
hlmc start lotus-storage-0 
```

### 运行矿工程序
```shell
cd $HOME/go/src/github.com/filecoin-project/lotus
./install.sh debug # 若是使用正式，执行./install.sh进行编译, 编译完成后自动放在$FILECOIN_BIN下
```

shell 1, 运行链
```shell
sudo rm -rf /data/sdb/lotus-user-1/.lotus* # 注意!!!! 需要确认此库不是正式库，删掉需要重新同步数据与创建矿工，若创世节点一样，可不删除。
cd ~/hlm-miner
. env.sh
cd ~/hlm-miner/apps/lotus
cp ~/hlm-miner/etc/hlmd/apps/tpl/lotus-daemon-0.ini ~/hlm-miner/etc/hlmd/apps/ # 若原已存在，不需再复制过来
hlmc reload
# 生产上运行前注意修改./daemon.sh脚本中的netip地址段，默认只支持10段
hlmc start lotus-daemon-1 # 或者可手动运行./daemon.sh进行调试
hlmc tail lotus-daemon-1 stderr -f # 看日志
```

shell 2, 创建2k私网矿工
```shell
sudo rm -rf /data/sdb/lotus-user-1/.lotusstorage/* # 注意确认是否可删除
cd ~/hlm-miner/script/lotus/lotus-user/
. env/lotus-1.sh
. env/miner-1.sh
./init-miner-dev.sh
./miner.sh init --owner=xxxx --sector-size=2KiB # 注意修改miner.sh中的识别到的netip，默认只支持10地址段
```

shell 3, 运行矿工
```shell
cd ~/hlm-miner
. env.sh
cd ~/hlm-miner/apps/lotus
cp ~/hlm-miner/etc/hlmd/apps/tpl/lotus-user-0.ini ~/hlm-miner/etc/hlmd/apps/ # 若原已存在，不需再复制过来
hlmc reload
hlmc start lotus-user-1 # 或者可手动运行./miner.sh进行调试
hlmc tail lotus-user-1 stderr -f #看日志
```

shell 4，导入存储节点与密封扇区
```shell
cd ~/hlm-miner
. env.sh
cd ~/hlm-miner/script/lotus/lotus-user/
. env/lotus-1.sh
. env/miner-1.sh

# 添加存储节点(含sealed与unsealed存储在里边)
./init-storage-dev.sh # 会导入前面搭建的lotus-storage-0, 指令细节看shell里的代码

# 运行刷密封扇区
./miner.sh pledge-sector start

# 因在v1.10.0中使用批量提交消息的功能，在开发环境中默认很难达到默认批量提交配置要求，需要手动提交消息或修改配置文件
./miner.sectors batching precommit # 若有需要发布的数据，执行以下提交上链命令
./miner.sh sectors batching precommit --publish-now # 手动提交precommit消息

./miner.sectors batching commit # 若有需要发布的数据，执行以下提交上链命令
./miner.sh sectors batching commit --publish-now # 手动提交commit消息

或者修改配置文件(config.toml)
[Sealing]
#  MaxWaitDealsSectors = 2
#  MaxSealingSectors = 0
#  MaxSealingSectorsForDeals = 0
#  MaxDealsPerSector = 0
#  WaitDealsDelay = "6h0m0s"
#  AlwaysKeepUnsealedCopy = true
#  BatchPreCommits = true
  MaxPreCommitBatch = 12 # 满12任务提交一次
  MinPreCommitBatch = 1
  PreCommitBatchWait = "1h0m0s" # 超时1小时提交一次
#  PreCommitBatchSlack = "3h0m0s"
#  AggregateCommits = true
  MaxCommitBatch = 12 # 满12任务提交一次
  MinCommitBatch = 1
  CommitBatchWait = "1h0m0s" # 超时1小时提交一次
#  CommitBatchSlack = "1h0m0s"
#  TerminateBatchMax = 100
#  TerminateBatchMin = 1
#  TerminateBatchWait = "5m0s"
#

# miner的其他指令，参阅
./miner.sh --help
```

shell 4, 运行wdpost worker
```
cd ~/hlm-miner
. env.sh
cd ~/hlm-miner/apps/lotus
cp ~/hlm-miner/etc/hlmd/apps/tpl/lotus-worker-wdpost.ini ~/hlm-miner/etc/hlmd/apps/ # 若原已存在，不需再复制过来
hlmd ctl reload
hlmc start lotus-worker-wdpost # 或者手动调试./worker-wdpost.sh
hlmc tail lotus-worker-wdpost stderr -f # 看日志
```
*注意，因miner默认配置文件可能已打开(默认已打开)强制使用wdpost worker的选项，此项在此版本标注后默认需要运行wdpost worker，否则无法完成wdpost证明*  
请检查miner配置config.toml是否已打开以下开关，若已打开，将强制使用worker计算wdpost
```
[Storage]
#  ParallelFetchLimit = 10
#  AllowAddPiece = true
#  AllowPreCommit1 = true
#  AllowPreCommit2 = true
#  AllowCommit = true
#  AllowUnseal = true
RemoteSeal = true
RemoteWnPoSt = 2
RemoteWdPoSt = 2
EnableForceRemoteWindowPoSt = true
#
[Fees]
#  MaxPreCommitGasFee = "0.025 FIL"
#  MaxCommitGasFee = "0.05 FIL"
#  MaxTerminateGasFee = "0.5 FIL"
#  MaxWindowPoStGasFee = "5 FIL"
#  MaxPublishDealsFee = "0.05 FIL"
#  MaxMarketBalanceAddFee = "0.007 FIL"
EnableSeparatePartition = true
PartitionsPerMsg = 4 # 根据单台计算wdpost的分区数能力而设定，2080ti GPU官方proof版本约支持为4个分区/台
```

shell 5, 运行wnpost worker
```
cd ~/hlm-miner
. env.sh
cd ~/hlm-miner/apps/lotus
cp ~/hlm-miner/etc/hlmd/apps/tpl/lotus-worker-wnpost.ini ~/hlm-miner/etc/hlmd/apps/ # 若原已存在，不需再复制过来
hlmd ctl reload
hlmc start lotus-worker-wnpost # 或者手动调试./worker-wdpost.sh
hlmc tail lotus-worker-wnpost stderr -f # 看日志
```
shell 6, 运行p1 worker
```
cd ~/hlm-miner
. env.sh
cd ~/hlm-miner/apps/lotus
cp ~/hlm-miner/etc/hlmd/apps/tpl/lotus-worker-1t.ini ~/hlm-miner/etc/hlmd/apps/ # 若原已存在，不需再复制过来
hlmd ctl reload
hlmd ctl start lotus-worker-1t
hlmd ctl tail lotus-worker-1t stderr -f # 查日志
```

shell 7, 运行c2 worker
```
cd ~/hlm-miner
. env.sh
cd ~/hlm-miner/apps/lotus
cp ~/hlm-miner/etc/hlmd/apps/tpl/lotus-worker-c2.ini ~/hlm-miner/etc/hlmd/apps/ # 若原已存在，不需再复制过来
hlmd ctl reload
hlmd ctl start lotus-worker-c2
hlmd ctl tail lotus-worker-c2 stderr -f # 查日志
```

## 启用链集群(可选)
未完成上述前不涉及到集群, 需要对项目已深入后才能运行以下部署。

链接入etcd部署的图, etcd-gateway需要在链节点上启动, 同一个物理节点的进程共用一个gateway
```text
ectd0   etcd1   etcd2
    \     |     /
    etcd-gateway(127.0.0.1:2379)
          |
        lotus
```


配置/etc/hosts
```
127.0.0.1 bootstrap0.etcd.grandhelmsman.com
127.0.0.1 bootstrap1.etcd.grandhelmsman.com
127.0.0.1 bootstrap2.etcd.grandhelmsman.com
```

启动etcd服务, 此处为三个节点在同一台机器上部署
```
hlmd ctl start etcd-bootstrap-0 
hlmd ctl start etcd-bootstrap-1
hlmd ctl start etcd-bootstrap-2 
hlmd ctl start etcd-gwateway
```

启动链集群
```
hlmd ctl stop lotus-daemon-e1 # repo=/data/sdb/lotus-user-1/.lotus
hlmd ctl stop lotus-daemon-e2 # repo=/data/cache/.lotus
```

在miner的使用的链repo(默认在/data/sdb/lotus-user-1/.lotus)下lotus.proxy,   
通过以下指令创建(需miner已运行)
```shell
cd ~/hlm-miner/scripts/lotus/lotus-user
./miner.sh proxy create
```
会自动根据repo下的原api与token生成lotus.proxy格式如下：
```
# the first line is for proxy addr
eyJhbGciOiJIUzI1NiIsInR5cCI6IkpXVCJ9.eyJBbGxvdyI6WyJyZWFkIiwid3JpdGUiLCJzaWduIiwiYWRtaW4iXX0.ONUWYvPk-nAgLXy-FeN-O-CoMeHIUGWrywC6QMdTjag:/ip4/127.0.0.1/tcp/0/http
# the following are the lotus nodes seperate with \n
eyJhbGciOiJIUzI1NiIsInR5cCI6IkpXVCJ9.eyJBbGxvdyI6WyJyZWFkIiwid3JpdGUiLCJzaWduIiwiYWRtaW4iXX0.ONUWYvPk-nAgLXy-FeN-O-CoMeHIUGWrywC6QMdTjag:/ip4/127.0.0.1/tcp/11234/http
```

重新启动后即可自动使用集群, 通过log或者链代理可查看状态
```shell
hlmd ctl restart lotus-user-1
./miner.sh proxy status
```

## 目录规范

项目涉及到的所有目录，以下这些目录将在单机部署上建立
```
/data -- 项目数据目录

# 缓存盘
/data/cache -- 缓存盘，必要时此盘数据会被清除，存放的数据要求是可损坏的，可单独挂载盘，建议挂载ssd盘
/data/cache/.lotus -- 链数据目录，因实际产线需要高速的盘而在链节点上启用此目录，可损坏，注意备份钱包密钱
/data/cache/.lotusworker -- lotus-seal-worker计算缓存目录，计算结束后会自动清除, 若lotus-cache下未挂载盘时，默认会使用此目录缓存计算结果
/data/cache/filecoin-parents -- filecoin worker计算时的高速辅助目录
/data/cache/filecoin-proof-parameters -- filecoin本地启动参数版本管理目录文件，此文件数据需要65G左右的空间
/data/cache/filecoin-proof-parameters/v28 -- filecoin本地启动参数目录实际目文件
/data/cache/tmp -- 程序$TMPDIR设定的目录
/data/lotus-push -- 计算结果推送目录，会自动单独挂载盘，可选
/data/lotus-cache -- 新的缓存盘结构
/data/lotus-backup -- 异地备份使用的目录入口

# 矿工数据盘
/data/sd(?) -- 矿工存储数据目录(前期设计多进程时对应多盘位), 可单独挂载盘，默认为/data/sdb
/data/sd(?)/lotus-user-(?)/.lotus -- lotus矿工绑定的数据链目录, 可单独挂载盘, 产线实际为软链, 默认为/data/sdb/lotus-user-1/.lotus
/data/sd(?)/lotus-user-(?)/.lotusstorage -- lotus矿工存储数据目录, 可单独挂载盘, 产线实际为软链, 默认为/data/sdb/lotus-user-1/.lotusstorage


# 存储链接入口
/data/zfs -- 挂载zfs池到本地的目录
/data/nfs -- 挂载nfs文件的目录

# 启动参数链接入口
/var/tmp/filecoin-proof-parameters # filecoin启动参数文件入口，会被软连接到/data/cache/filecoin-proof-parameters对应版本下
```

### 存储节点上的目录

```
/data/zfs的目录自行挂载需要的盘
```

### 链节点目录
链同步节点目录, 用于存储区块链数据，长期考虑，应留1T的链数据空间或挂载为长期的存储盘，应与矿工数据分离存储
```text
/data/sd(?)/lotus-user-x/.lotus # 默认为/data/sdb/lotus-user-1/.lotus

# 因产线需要，当前链的性能要求高，产线上需要ssd来加速，高速盘将挂载在/data/cache下
/data/cache/.lotus
sudo ln -s /data/cache/.lotus /data/sdb/lotus-user-1/.lotus
```
### 矿工节点目录

在miner节点中，会用到三种级别的目录

链api目录, 若是同一台机器，不需要新建
```text
/data/sd(?)/lotus-user-x/.lotus # 默认为/data/sdb/lotus-user-1/.lotus
```

矿工元数据节点目录, 用于引导miner的启动
```text
/data/sd(?)/lotus-user-x/.lotusstorage # 默认为/data/sdb/lotus-user-1/.lotus

# 因产线需要灾备, 重定向了矿工元数据目录
zfs create data-zfs/.lotusstorage
sudo ln -s /data/zfs/.lotusstorage /data/sdb/lotus-user-1/.lotusstroage
```

存力存储目录, 用于实际存储存力, 由矿工节点自动进行管理与挂载  
因历史原因，/data/nfs这个目录已被固化
```text
/data/nfs/1
/data/nfs/2
/data/nfs/3
```

### 计算节点目录

在worker中，需要用到四个目录

worker 配置文件目录
```text
~/.lotusworker
```

矿工api配置文件目录，用于启动worker, 若在同一台机器上，不需要新建
```text
/data/sdb/lotus-user-1/.lotusstorage
```

工作者配置文件目录，用于缓存存储临时密封的数据, 应使用高速io盘，以便提高本地的io吞吐
```text
/data/cache/.lotusworker
```

密封结果推送目录(启用lotus-storage后此目录已不用，若用回mount类传输，则仍需要此目录)
```text
/data/lotus-push

worker程序会根据miner分配的存储节点配置自动挂载到此目录
```

## 发布二进制
请参考[hlm-miner部署文档](https://github.com/filecoin-fivestar/hlm-miner/apps/lotus/README.md)

## 附录

[开发IDE](https://github.com/filecoin-fivestar/ide)
