worker 运行说明文档

参数说明
```
--worker-repo
    worker工作目录
--miner-repo
    矿工api与token目录
--storage-repo
    密封结果推送目录，当与worker工作目录相同时, 应启用--cache-mode=1的参数
--id-file
    worker的ID文件位置，用于避开共同一个cache的问题。默认指向~/.lotusworker/worker.id
--listen-addr
    worker对外服务的绑定地址，格式为: ip:port
--cache-mode
    0为本地ssd计算模式，需要传输数据；1为共享目录的方式，不会触发数据传输。
--max-tasks
    所有可运行的任务数，根据硬盘可承载的扇区容量配置
--transfer-buffer
    密封结束后缓存等待后台传输的缓存扇区个数，此值加上max-tasks会得到磁盘的最大总个数, 默认值为1个缓存
--parallel-pledge
    可并行的最大抵押addpiece消费者数量，0关闭功能。注意：多个addpiece运行时会降低addpiece的速度。需要配合miner的RemoteSeal为true。
--parallel-precommit1
    可并行的最大precommit1消费者数量, 0关闭功能。注意，远程Unseal会共用此值，需要配合miner的RemoteSeal为true。
--parallel-precommit2
    可并行的最大precommit2消费者数量，默认值为1，串行执行p2; 0关闭功能。需要配合miner的RemoteSeal为true。
--parallel-commit
    可并行的最大commit消费者数量，默认值为1，串行执地c2; 0为调用远程c2。需要配合miner的RemoteSeal为true。
--commit2-srv
    是否开启commit2服务功能 
--wdpost-srv
    是否开启window post服务, 需要配合miner的RemoteWdPoSt数值使用，RemoteWdPoSt的数值代表同时计算的台数，用于提高计算的可靠性。
--wnpost-srv
    是否开启winning post服务, 需要配合miner的RemoteWnPoSt数值使用，RemoteWnPoSt的数值代表同时计算的台数，用于提高计算的可靠性。
```

例子1, 默认单任务全流程运行，启用1个传输缓冲(缓冲时会接新的pledge任务)，适用于1T盘，等价于以下用例
```shell
netip=$(ip a | grep -Po '(?<=inet ).*(?=\/)'|grep -E "10\.") # only support one eth card.
RUST_LOG=info RUST_BACKTRACE=1 NETIP=$netip ./lotus-seal-worker --repo=$repo --storagerepo=$storagerepo --sealedrepo=$sealedrepo run 
```

例子2, 单任务运行，不启用传输缓冲，适用于500GB盘：
```shell
netip=$(ip a | grep -Po '(?<=inet ).*(?=\/)'|grep -E "10\.") # only support one eth card.
RUST_LOG=info RUST_BACKTRACE=1 NETIP=$netip ./lotus-seal-worker --repo=$repo --storagerepo=$storagerepo --sealedrepo=$sealedrepo --max-tasks=1 --cache-mode=0 --transfer-buffer=0 --parallel-pledge=1 --parallel-precommit1=1 --parallel-precommit2=1 --parallel-commit=1 run 
```

例子3, 12任务运行，启用传输缓冲，串行p2与c2, 适用于1T内存，8T盘：
```shell
netip=$(ip a | grep -Po '(?<=inet ).*(?=\/)'|grep -E "10\.") # only support one eth card.
RUST_LOG=info RUST_BACKTRACE=1 NETIP=$netip ./lotus-seal-worker --repo=$repo --storagerepo=$storagerepo --sealedrepo=$sealedrepo --max-tasks=12 --cache-mode=0 --transfer-buffer=1 --parallel-pledge=12 --parallel-precommit1=12 --parallel-precommit2=1 --parallel-commit=1 run 
```

例子4, 12任务运行，启用传输缓冲，串行p2与c2, 适用于1T内存，10000个任务硬盘容量：
```shell
netip=$(ip a | grep -Po '(?<=inet ).*(?=\/)'|grep -E "10\.") # only support one eth card.
RUST_LOG=info RUST_BACKTRACE=1 NETIP=$netip ./lotus-seal-worker --repo=$repo --storagerepo=$storagerepo --sealedrepo=$sealedrepo --max-tasks=10000 --cache-mode=0 --transfer-buffer=1 --parallel-pledge=12 --parallel-precommit1=12 --parallel-precommit2=1 --parallel-commit=1 run 
```

变更列表:
```

2020-12-17
-----------------------------
*, 变更addpiece为pledge，并全部实现pledge功能(含存储市场)可远程到worker上并行计算; 在实现中，addpiece为添加数据片，worker执行的计算实际是addpiece后的pledge功能;
*, 合并c1与c2功能，c1完成后直接在worker上调用c2服务，调用c2的相关流量为两worker直通，不再由miner中转;
*, 增加存储市场的远程unseal功能, 注意需要依赖于独立的unseal存储节点(详见lotus-miner hlm-storage add --help);


2020-07-21
-----------------------------
*, 从单任务变更为多任务并行
*, 存储节点分离出信令ip与传输ip, 信令ip用于miner信令通信，传输ip用于worker数据传输
*, 扇区状态变更：
   	WorkerAddPiece       WorkerTaskType = 0
	WorkerAddPieceDone                  = 1
	WorkerPreCommit1                    = 10
	WorkerPreCommit1Done                = 11
	WorkerPreCommit2                    = 20
	WorkerPreCommit2Done                = 21
	WorkerCommit1                       = 30
	WorkerCommit1Done                   = 31
	WorkerCommit2                       = 40
	WorkerCommit2Done                   = 41
	WorkerFinalize                      = 50
*, 存储节点分配变更为后置分配，并支持优化本机传输。
*, 缓存管理变更，支持共享cache文件
*, 考虑多worker共用一个cache的问题，sealedrepo需要人工管理，并增加id-file的人工指定workerid文件, 详见:
   lotus-seal-worker --help
*, 启用c2 GPU服务
*, 启用window post服务
*, 启用wining post服务
```

