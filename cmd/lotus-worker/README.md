worker 运行说明文档

参数说明
```
--repo  worker工作目录
--storagerepo   矿工api与token目录
--sealedrepo    密封结果推送目录，当与worker工作目录相同时, 应启用--cache-mode=1的参数
--max-tasks     所有可运行的任务数，根据内存配置, 所有实际并行运行中的任务不会超过此值
--cache-mode    0为本地ssd计算模式，需要传输数据；1为共享目录的方式，不会触发数据传输。
--transfer-buffer 密封结束后缓存等待后台传输的缓存扇区个数，此值加上max-tasks会得到磁盘的最大总个数, 默认值为1个缓存
--parallel-addpiece 可并行的最大addpiece数，当多个阶段同时运行时，并行的数可能会达不到最大值, 0关闭功能。注意：多个addpiece运行时会降低addpiece的速
--parallel-precommit1 可并行的最大precommit1数，当多个阶段同时运行时，并行的数可能会达不到最大值, 0关闭功能
--parallel-precommit2 可并行的最大precommit2数，当多个阶段同时运行时，并行的数可能会达不到最大值, 默认值为1，串行执地p2; 0关闭功能.
--parallel-commit1 可并行的最大commit1数，当多个阶段同时运行时，并行的数可能会达不到最大值, 0关闭功能
--parallel-commit2 可并行的最大commit2数，当多个阶段同时运行时，并行的数可能会达不到最大值, 默认值为1，串行执地c2; 0关闭功能
```

例子1, 默认单任务运行，启用传输缓冲，适用于1T盘，等价于以下用例
```shell
netip=$(ip a | grep -Po '(?<=inet ).*(?=\/)'|grep -E "10\.") # only support one eth card.
RUST_LOG=info RUST_BACKTRACE=1 NETIP=$netip ./lotus-seal-worker --repo=$repo --storagerepo=$storagerepo --sealedrepo=$sealedrepo --max-tasks=1 --cache-mode=0 --transfer-buffer=1 --parallel-addpiece=1 --parallel-precommit1=1 --parallel-precommit2=1 --parallel-commit1=1 --parallel-commit2=1 run 
```

例子2, 单任务运行，不启用传输缓冲，适用于500GB盘：
```shell
netip=$(ip a | grep -Po '(?<=inet ).*(?=\/)'|grep -E "10\.") # only support one eth card.
RUST_LOG=info RUST_BACKTRACE=1 NETIP=$netip ./lotus-seal-worker --repo=$repo --storagerepo=$storagerepo --sealedrepo=$sealedrepo --max-tasks=1 --cache-mode=0 --transfer-buffer=0 --parallel-addpiece=1 --parallel-precommit1=1 --parallel-precommit2=1 --parallel-commit1=1 --parallel-commit2=1 run 
```

例子3, 12任务运行，启用传输缓冲，串行p2与c2, 适用于1T内存，8T盘：
```shell
netip=$(ip a | grep -Po '(?<=inet ).*(?=\/)'|grep -E "10\.") # only support one eth card.
RUST_LOG=info RUST_BACKTRACE=1 NETIP=$netip ./lotus-seal-worker --repo=$repo --storagerepo=$storagerepo --sealedrepo=$sealedrepo --max-tasks=14 --cache-mode=0 --transfer-buffer=1 --parallel-addpiece=14 --parallel-precommit1=14 --parallel-precommit2=1 --parallel-commit1=14 --parallel-commit2=1 run 
```

TODO:
```
*, 当启用cache-mode=1时，worker id不能放在共享目录中
*, 启用c2 GPU服务
*, 启用window post服务
*, 启用wining post服务
```

变更列表:
```
1, 从单任务变更为多任务并行
2, 存储节点分离出信令ip与传输ip, 信令ip用于miner信令通信，传输ip用于worker数据传输
3, 扇区状态变更：
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
4, 缓存管理变更 
5, 存储节点分配变更为后置分配，并支持优化本机传输。

```
