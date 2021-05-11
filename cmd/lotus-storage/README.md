# 文档说明

# 目录
- [前言](#前言)
- [鉴权系统](#鉴权系统)
    - [BasicAuth](#BasicAuth)
    - [TokenAuth](#TokenAuth)
    - [PosixAuth](#PosixAuth)
- [协议设计](#协议设计)
    - [BasicAuth协议接口](#BasicAuth协议接口)
    - [TokenAuth协议接口](#TokenAuth协议接口)
    - [PosixAuth协议接口](#PosixAuth协议接口)
- [调用用例](#调用用例)
    - [环境变量](#环境变量)
    - [测试指令](#测试指令)


## 前言
因使用nfs访问方式存在局域网存储访问隐患，故重写了存储鉴权结构以增强存储访问的安全性。

存储安全分物理存储安全及软件存储安全两大部份。

物理存储安全需要依靠上层应用做多副本存储，本节点中只提供单机型的raid6级的raid冗余，最大可损坏两块硬盘。

软件存储安全主要为访问安全，分本地访问与网络访问两部分。本地访问依赖于linux本身的安全；本设计主要针对网络访问安全而设计。

网络访问主要参考了oauth2的鉴权流程对外提供文件增、删、查、改功能，并根据filecoin的系统文件要求提供对应的posix规范综合进行了以下改造:

## 鉴权系统
```
鉴权系统主要提供了基本安全密钥生成，包括两部分：
BasicAuth 基本鉴权，需要通过https接入, 在miner机上进行管理。
TokenAuth 会话授权，通过BasicAuth换取每一次会话授权所需要的token，此权限可以通过http进行文件的增、删、查、改功能。 
PosixAuth Posix接口，因Filecoin需要Posix文件访问，此授权通过BasicAuth生成只读的token仅授权给miner、wdpost访问，此访问受BasicAuth管理。
```

### BasicAuth
```
存储节点初始化时，会有一个初始的密钥。
当存储节点被添加到miner节点时，miner通过初始密钥对存储节点的BasicAuth进行接管；
miner接管理BasicAuth后，重新重启miner会进行一次密钥变更，同时提供了手工变更BasicAuth密钥的功能，防止BasicAuth被强破的可能性。
miner管理存储节点的的BasicAuth时，每一台存储节点会一个唯一不同的密钥，不同的存储节点需要不同的BasicAuth访问。
miner需要被视为可信的物理节点，当miner节点不可信时，应及时变更存储节点密码，默认情况下重启miner即可变，不需人工介入。
```

### TokenAuth
```
此为Filecoin的每一个扇区访问安全而设计，即不同扇区每一次会话会有不同的TokenAuth，以确保每一个扇区的操作使用的是唯一密钥。
当需要操作存储节点上的某个扇区(上传、下载、读取、删除)时，需通过miner获取该扇区的会话授权token，使用完后自行释放，不释放时默认一定时后会自行释放。
此设计专为大文件传输而设计，小文件请使用PoxixAuth接口。
```

### PosixAuth
```
因filecoin本身是基于Posix的文件系统设计，涉及范围较广，不易修改源码，故基于go-fuse开源库改造了文件的访问方式，但文件访问难以控制每一个扇区的权限。
授权后默认为只读功能，打开写功能时，应只能授权给miner节点全用，关闭其他不可信节点的改写存储节点的能力。
PosixAuth的授权码基于BasicAuth生成，因此当PosixAuth不可信时，应及时变更BasicAuth，默认情况下重启miner即可，不需人工介入。
小文件读取会带来大量的上下文切换从而产生系统开销。此设计专为高并发小读取而设计，适用于多并发的小文件读取。
```

## 协议设计

### BasicAuth协议接口
```
指令接口，走https协议
/check -- 监控zfs磁盘状态，等价于zfs status -x, 只读
/sys/auth/change?token=d41d8cd98f00b204e9800998ecf8427e 首次使用时须调些接口重置密钥，成功时返回200及新密钥，已改过时返回401错误
/sys/file/token?sid=s-t0xxx-xx -- 获取临时的token
/sys/disk/capacity -- 获取磁盘容量
```


### TokenAuth协议接口
```
文件传输专用，因大文件而走http协议
/file/move?sid=s-t0xxx-xx -- 标记扇区为正常, 以便wdpost继续证明该扇区
/file/delete?sid=s-t0xxx-xx -- 删除扇区，注意，该操作不可恢复。
/file/list?file=xxx -- 列出文件的信息
/file/download?file=xxx&pos=0&checksum=sha1 -- 文件读取，HEAD请求为读取文件信息，GET为获取文件数据，因大文件而走http，但需要填写BaseAuth的username为s-t0xxx-xx,password为临时的token。checksum当前固定为sha1,其他值不返回hash值, 内置支持文件夹下载与自动断点续传
/file/upload?file=xxx&pos=0&checksum=sha1 -- POST, 文件上传，因大文件而走http，但需要填写BaseAuth的username为s-t0xxx-xx,password为临时的token;checksum当前固定为sha1,其他值不返回hash值, 内置支持文件夹上传与自动断点续传功作
```

### PosixAuth协议接口
```
基于TCP实现的FUSE接口，支持通过fuse挂载为posix文接口。
本实现分两部分，底层与应用层
底层为tcp接口操作;
应用层支持库接入与fuse接入
```

#### PosixAuth TCP底层接口设计
请求协议分为两种，一种为文本协议，一种为文件传输协议。

*TCP文本请求协议* 
```
支持以下功能
List -- 列出文件夹内容，用于实现fuse的目录查询
Stat -- 查询文件的属性
Cap  -- 查询挂载目录的大小，用于实现df功能
Open -- 打开一个文件，转入到文件传输模式

文本请求协议
byte[0]   -- 信令控制符
byte[1:5] -- 数据区长度
byte[5:n] -- 序列化的json文本
请求的json文本协议如下
{
   "Method":"方法",
   "参数1":"内容1",
   ...
}

文本响应协议
byte[0]   -- 信令控制符
byte[1:5] -- 数据区长度
byte[5:n] -- 序列化的json文本
响应的json文本协议如下
{
  "Code":"200", -- 响应码，200为成功
  "Data":{}, -- 响应的数据
  "Err":"", -- 错误信息补充
}

```

*TCP文件传输协议*

当进行Open打开文件模式时，客户端的请求会转入到此模式，用于传输文件。

文件传输模式下，最大支持并发数(client/fuse_conn.go#FUSE_CONN_POOL_SIZE_MAX)为15个文件打开;

文件传输模式下，因频繁读写触发TCP的切换，会降低传输速率，提高读写的buffer值可提高传输速率。

基于lotus-storage支持http接口上传与下载，在千兆口的32GB数据下载测试中，

http(100+MB/s)>nfs(80+MB/s)>fuse(70+MB/s)。

基于lotus-storage支持fuse文件系统

wdpost 32GB测试中(nfs 12+分钟, fuse 8+分钟
```
支持以下功能：
Tuncate -- 文件的Truncate功能，用于兼容官方存储市场的存储实现
Stat    -- 文件属性功能
Write   -- 文件写入功能，需要写入权限(GetAuthRW)
Read    -- 文件读取功能
ReadAt  -- 文件指定位置读取功能
Seek    -- 文件读写指针位置设定
Close   -- 文件关闭功能，在服务器端，若10分无文件操作，会自动关闭已打开的文件，转入缓存状态，缓存状态在权限变化时会被清空，该缓存的设计为可删除设计。

文件传输请求协议
byte[0]   -- 信令控制符
byte[1:5] -- 数据区长度
byte[5:5+16] -- 文件ID
byte[21:n] -- 二进制数据区

文件传输响应协议
byte[0]   -- 信令控制任
byte[1:5] -- 数据区长度
byte[5:n] -- 二进制数据区
```

#### PosixAuth 应用层接口设计
应用分为库应用与fuse挂载应用

库应用为构建FUseFile实例进行应用，详见client/fuse_file_test.go

fuse挂载应用, 挂载为fuse后，通过标准文件进行读取，当前只支持只读。

系统需要安装fuse库(apt-get install fuse)

```
import "github.com/filecoin-project/lotus/cmd/lotus-storage/client"

func main() {
	authData := GetAuthRO()
	fuc := client.NewFUseClient(_posixFsApiFlag, authData)
	if err := fuc.Mount(cctx.Context, mountPoint); err != nil {
        panic(err)
	}
	end := make(chan os.Signal, 2)
	signal.Notify(end, os.Interrupt, os.Kill, syscall.SIGTERM, syscall.SIGINT)
	<-end
}
```


## 调用用例

### 环境变量
```
LOTUS_FUSE_DEBUG=1 # 开启FUSE调试日志，适用于服务端与客户端
# 设定客户端可以连接服务端的连接池大小, 此值越大，支持并发连接越多，但当存在大量存储服务器时，需要注意客户端端口上限的问题。
# POOL_SIZE=30，实际连接数约为60左右，即miner默认的可用端口数(约30000个)可支持500台左右的存储节点。
LOTUS_FUSE_POOL_SIZE=30 
```

### 测试指令
将auth.dat复制到lotus-storage同一目录下，或自行通过lotus-storage --storage-root指定
```
LOTUS_FUSE_DEBUG=1 lotus-storage daemon # 运行存储服务程序
LOTUS_FUSE_DEBUG=1 LOTUS_FUSE_POOL_SIZE=15 lotus-storage mount [mountpoint] # 通过fuse挂载到本地目录
lotus-storage download [remote path] [local path] 下载文件到本地
lotus-storage upload [local path] [remote path] 上传本地文件到服务器
```
