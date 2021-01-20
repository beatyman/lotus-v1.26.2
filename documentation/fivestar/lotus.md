# 搭建开发环境

- [开发环境安装](#开发环境安装)
- [国内安装技巧](#国内安装技巧)
- [下载lotus源代码](#下载lotus源代码)
- [创建本地开发环境](#搭建创世节点)
- [启用链集群](#启用链集群)
- [目录规范](#目录规范)
    - [存储节点上的目录](#存储节点上的目录)
    - [链节点目录](#链节点目录)
    - [矿工节点目录](#矿工节点目录)
    - [计算节点目录](#计算节点目录)
- [发布二进制](#发布二进制)
- [附录](#附录)

## 开发环境安装
```shell
# 安装依赖(需要ubuntu 18.04)
apt-get update
apt-get install aptitude
aptitude install rsync chrony nfs-common make mesa-opencl-icd ocl-icd-opencl-dev gcc git bzr jq pkg-config curl clang build-essential libhwloc-dev
```

## 国内安装技巧 
参考: https://docs.lotu.sh/en+install-lotus-ubuntu

### 1), 安装go
```shell
sudo su -
cd /usr/local/
wget https://studygolang.com/dl/golang/go1.15.5.linux-amd64.tar.gz # 其他版本请参考https://studygolang.com/dl
#wget https://golang.org/dl/go1.15.5.linux-amd64.tar.gz
#scp root@10.1.1.33:/root/rsync/go1.15.5.linux-amd64.tar.gz .

tar -xzf go1.15.5.linux-amd64.tar.gz
### 配置/etc/profile环境变量(需要重新登录生效或source /etc/profile)
export GOROOT=/usr/local/go
export GOPROXY="https://goproxy.io,direct"
export GOPRIVATE="github.com/filecoin-fivestar"
export GIT_TERMINAL_PROMPT=1
export PATH=$GOROOT/bin:$PATH:/bin:/usr/local/sbin:/usr/local/bin:/usr/sbin:/usr/bin:/sbin:/bin:/usr/games:/usr/local/games:/snap/bin
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

## 编译lotus源代码
```shell
mkdir -p $HOME/go/src/github.com/filecoin-project
cd $HOME/go/src/github.com/filecoin-project
# 下载慢时注意配置/etc/hosts
# https://blog.csdn.net/random0708/article/details/106001665/
git clone --origin fivestar https://github.com/filecoin-fivestar/fivestar-lotus.git lotus
cd lotus
git checkout testing # 检出需要的分支
# 编译
make clean
env RUSTFLAGS="-C target-cpu=native -g" CGO_CFLAG="-D__BLST_PORTABLE__" FFI_BUILD_FROM_SOURCE=1 make
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
./install.sh install # hlmd ctl status # 有状态输出为成功


```

## 创建本地开发网络

### 搭建创世节点
```shell
cd $HOME/go/src/github.com/filecoin-project/lotus
./clean-bootstrap.sh
ps axu|grep lotus # 确认所有相关进程已关闭
./init-bootstrap.sh
tail -f bootstrap-init.log # 直到'nohup ./scripts/run-genesis-miner.sh', ctrl+c 退出, 进一步查询bootstrap-miner.log
ssh-keygen -t ed25519 # 创建本机ssh密钥信息，已有跳过
# 修改PermitRootLogin为yes, 参考 https://blog.csdn.net/zilaike/article/details/78922524
ssh-copy-id root@127.0.0.1
./deploy-boostrap.sh # 部署水龙头及对外提供的初始节点
# 遇错时从clean-boostrap重新开始

# 重启创世节点
ps axu|grep lotus
kill -9 xxxx # 相关进程pid
nohup ./scripts/run-genesis-lotus.sh >> bootstrap-lotus.log 2>&1 & # 启动创世节点链
nohup ./scripts/run-genesis-miner.sh >> bootstrap-miner.log 2>&1 & # 启动创世节点矿工

```

### 搭建存储节点
```shell
aptitude install nfs-server
mkdir -p /data/nfs
mkdir -p /data/zfs
mkdir -p /data/zfs/cache
mkdir -p /data/zfs/sealed
chattr -V +a /data/zfs # 读写权限，不能删除
chattr -V +a /data/zfs/cache # 读写权限，不能删除
chattr -V +a /data/zfs/sealed # 读写权限，不能删除

echo "/data/zfs/ *(rw,sync,insecure,no_root_squash)" >>/etc/exports
systemctl reload nfs-server
showmount -e # 校验NFS是否已共享出来
```

### 生成开发版lotus程序
```shell
cd $HOME/go/src/github.com/filecoin-project/lotus
./install.sh debug # 若是使用正式，执行./install.sh进行编译, 编译完成后自动放在$FILECOIN_BIN下
rm -rf /data/sdb/lotus-user-1/.lotus* # 注意!!!! 需要确认此库不是正式库，删掉需要重新同步数据与创建矿工，若创世节点一样，可不删除。
```

shell 1, 运行链
```shell
cd ~/hlm-miner/apps/lotus
# 运行前注意修改脚本中的netip地址段，默认只支持10段
./daemon.sh # 或者直接hlmd ctl start lotus-daemon-1, hlmd ctl tail lotus-daemon-1 stderr -f 看日志
```

shell 2, 创建私网矿工
```shell
cd ~/hlm-miner/script/lotus/lotus-user/
. env/lotus.sh
. env/1.sh
./init-miner-dev.sh
./miner.sh init --sector-size=2KiB # 注意修改miner.sh中的识别到的netip，默认只支持10地址段
```

shell 3, 运行矿工
```shell
cd ~/hlm-miner/apps/lotus
./miner.sh # 或者直接hlmd ctl start lotus-user-1,hlmd ctl tail lotus-user-1 stderr -f 看日志
```

shell 4，导入存储节点
```shell
cd ~/hlm-miner/script/lotus/lotus-user/

# 添加存储节点(含sealed与unsealed存储在里边)
./init-storage-dev.sh

# 运行刷量
./miner.sh pledge-sector start

# miner的其他指令，参阅
./miner.sh --help
```

shell 4, 运行wdpost
```
cd ~/hlm-miner/apps/lotus
./worker-wdpost.sh # 或者直接hlmd ctl start lotus-worker-wdpost, hlmd ctl tail lotus-worker-wdpost stderr -f 看日志
```

shell 5, 运行worker
```
cd ~/hlm-miner/apps/lotus
# 运行前注意修改脚本中的netip地址段，默认只支持10段
./worker.sh # 或者直接hlmd ctl start lotus-worker-1
```
shell 6，刷扇区
```
cd ~/hlm-miner/script/lotus/lotus-user/

# 运行刷量
./miner.sh pledge-sector start

# miner的其他指令，参阅
./miner.sh --help
```

## 启用链集群

(此为开发可选项)

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

配置lotus接入到etcd集群
```
lotus daemon --etcd="http://127.0.0.1:2379" # 在apps/lotus/daemon.sh里配置
# 重启链
```

配置miner接入到多个链节点
```
mkdir -p $miner_repo # 自行填写此目录变量，默认为/data/sdb/lotus-user-1/.lotus
cd $miner_repo
echo "# the first line is for proxy addr">lotus.proxy
echo $(cat token)":/ip4/127.0.0.1/tpc/1345/http">>lotus.proxy
echo "# bellow is the cluster node.">>lotus.proxy
echo $(cat token)":"$(cat api)>>lotus.proxy
# 重启miner
```

## 目录规范

项目涉及到的所有目录，以下这些目录将在单机部署上建立
```
/data -- 项目数据目录

# 缓存盘
/data/cache -- 缓存盘，必要时此盘数据会被清除，存放的数据要求是可损坏的，可单独挂载盘，建议挂载ssd盘
/data/cache/filecoin-proof-parameters -- filecoin本地启动参数版本管理目录文件，此文件数据需要65G左右的空间
/data/cache/filecoin-proof-parameters/v28 -- filecoin本地启动参数目录实际目文件
/data/cache/.lotusworker -- lotus-seal-worker计算缓存目录，计算结束后会自动清除，需要1T左右空间
/data/cache/tmp -- 程序$TMPDIR设定的目录
/data/lotus-push -- 计算结果推送目录，会自动单独挂载盘，可选
/data/lotus-cache -- 新的缓存盘结构
/data/lotus-backup -- 备份使用的目录入口

# 矿工数据盘
/data/sd(?) -- 矿工存储数据目录(前期设计多进程时对应多盘位), 可单独挂载盘，默认为/data/sdb
/data/sd(?)/lotus-user-1/.lotus -- lotus矿工绑定的数据链目录, 可单独挂载盘, 默认为/data/sdb/lotus-user-1/.lotus
/data/sd(?)/lotus-user-1/.lotusstorage -- lotus矿工存储数据目录, 可单独挂载盘, 默认为/data/sdb/lotus-user-1/.lotusstorage


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

配置nfs的/etc/exports文件,进行nfs导出
```
/data/zfs/ *(rw,sync,insecure,no_root_squash)
```

### 链节点目录
链同步节点目录, 用于存储区块链数据，长期考虑，应留1T的链数据空间或挂载为长期的存储盘，应与矿工数据分离存储
```text
/data/sd(?)/lotus-user-x/.lotus # 默认为/data/sdb/lotus-user-1/.lotus
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
```

存力存储目录, 用于实际存储存力, 由矿工节点自动进行管理与挂载
```text
/data/nfs/1
/data/nfs/2
/data/nfs/3
```

### 计算节点目录

在worker中，需要用到三个目录

矿工api配置文件目录，用于启动worker, 若在同一台机器上，不需要新建
```text
/data/sd(?)/lotus-user-1/.lotusstorage
```

工作者配置文件目录，用于缓存存储临时密封的数据, 应使用高速io盘，以便提高本地的io吞吐
```text
/data/cache/.lotusworker
```

密封结果推送目录
```text
/data/lotus-push

worker程序会根据miner分配的存储节点配置自动挂载到此目录
```

## 发布二进制
将发布到./deploy/lotus
```
./publish.sh linux-amd64-amd
```

## 附录

[开发IDE](https://github.com/filecoin-fivestar/ide)
