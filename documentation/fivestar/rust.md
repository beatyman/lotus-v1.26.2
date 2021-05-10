# 搭建开发环境

- [开发环境安装](#开发环境安装)
- [国内安装技巧](#国内安装技巧)
- [下载lotus源代码](#下载lotus源代码)
- [调试RUST](#调试RUST)
- [附录](#附录)

## 开发环境安装
```shell
# 安装依赖(需要ubuntu 18.04)
apt-get update
apt-get install aptitude
aptitude install chrony nfs-common gcc git bzr jq pkg-config mesa-opencl-icd ocl-icd-opencl-dev libclang-dev
```

## 国内安装技巧 
参考: https://docs.lotu.sh/en+install-lotus-ubuntu

### 1), 安装go
```shell
sudo su -
cd /usr/local/
wget https://studygolang.com/dl/golang/go1.14.4.linux-amd64.tar.gz # 其他版本请参考https://studygolang.com/dl
tar -xzf go1.14.4.linux-amd64.tar.gz
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
export RUSTUP_DIST_SERVER=https://mirrors.ustc.edu.cn/rust-static
export RUSTUP_UPDATE_ROOT=https://mirrors.ustc.edu.cn/rust-static/rustup

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

## 下载lotus源代码
```shell
mkdir -p $HOME/go/src/github.com/filecoin-project
cd $HOME/go/src/github.com/filecoin-project
git clone https://github.com/filecoin-fivestar/lotus.git lotus
cd lotus
# 编译
make clean
env RUSTFLAGS="-C target-cpu=native -g" CGO_CFLAG="-D__BLST_PORTABLE__" FFI_BUILD_FROM_SOURCE=1 make
```

## 调试RUST
```shell
mkdir -p $HOME/go/src/github.com/filecoin-project
cd $HOME/go/src/github.com/filecoin-project
git clone https://github.com/filecoin-fivestar/lotus.git lotus
git clone https://github.com/filecoin-project/rust-fil-proofs.git
git clone https://https://github.com/filecoin-project/rust-filecoin-proofs-api.git
```
### 在rust-fil-proofs下测试
``` 
cd $HOME/go/src/github.com/filecoin-project/rust-fil-proofs
RUST_BACKTRACE=1 RUST_LOG=info FIL_PROOFS_USE_GPU_TREE_BUILDER=1 FIL_PROOFS_USE_GPU_COLUMN_BUILDER=1 cargo run --release --bin benchy -- stacked --size 2
```
### 在lotus下测试
1, 修改lotus/extern/filecoin-ffi/rust/Cargo.toml指向
```
[dependencies.filecoin-proofs-api]
package = "filecoin-proofs-api"
#version = "4.0.2"
path = "../../../../rust-filecoin-proofs-api"
```

2, 切换rust-filecoin-proofs-api版本与指向
```shell
cd $HOME/go/src/github.com/filecoin-project/rust-filecoin-proofs-api
git checkout v4.0.2 # 需要与lotus使用的同一版本
```

修改rust-filecoin-proofs-api/Cargo.toml指向
```
[dependencies]
anyhow = "1.0.26"
serde = "1.0.104"
paired = "0.20.0"
#filecoin-proofs-v1 = { package = "filecoin-proofs", version = "4.0.2" }
filecoin-proofs-v1 = { package = "filecoin-proofs", path = "../rust-fil-proofs/filecoin-proofs" }
```

3, 切换rust-fil-proofs版本与指向
```shell
cd $HOME/go/src/github.com/filecoin-project/rust-filecoin-proofs-api
git checkout releases/v4.0.2 # 需要与rust-filecoin-proofs-api使用的同一版本
```

4, 编译lotus基测程序
```shell
cd $HOME/go/src/github.com/filecoin-project/lotus
make clean
env RUSTFLAGS="-C target-cpu=native -g" FFI_BUILD_FROM_SOURCE=1 make bench
./bensh.sh
```

## 附录

[开发IDE](https://github.com/filecoin-fivestar/ide)
