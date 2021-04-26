// 文档说明
//
//
// 指令接口，走https协议
// /check -- 监控zfs磁盘状态，等价于zfs status -x, 只读
// /sys/auth/change?token=d41d8cd98f00b204e9800998ecf8427e 首次使用时须调些接口重置密钥，成功时返回200及新密钥，已改过时返回401错误
// /sys/file/token?sid=s-t0xxx-xx -- 获取临时的token
// /sys/disk/capacity -- 获取磁盘容量
//
//
// 文件传输专用，因大文件而走http协议
// /file/move?sid=s-t0xxx-xx -- 标记扇区为正常, 以便wdpost继续证明该扇区
// /file/delete?sid=s-t0xxx-xx -- 删除扇区，注意，该操作不可恢复。
// /file/list?file=xxx -- 列出文件的信息
// /file/download?file=xxx&pos=0&checksum=sha1 -- 文件读取，HEAD请求为读取文件信息，GET为获取文件数据，因大文件而走http，但需要填写BaseAuth的username为s-t0xxx-xx,password为临时的token。checksum当前固定为sha1,其他值不返回hash值
// /file/upload?file=xxx&pos=0&checksum=sha1 -- POST, 文件上传，因大文件而走http，但需要填写BaseAuth的username为s-t0xxx-xx,password为临时的token;checksum当前固定为sha1,其他值不返回hash值
//
// nfs授权限只读, 仅为兼容lotus文件读取而提供的接口
// 默认挂载为只读("token""read")
// mount -t nfs -o port=1332,mountport=1332,nfsvers=3,noacl,tcp,nolock,intr,rsize=1048576,wsize=1048576,hard,timeo=7,retrans=10,actimeo=10,retry=5 localhost:/2b60d59b5862e7232887de50bc1dddc3 mountpoint
// 挂载为读写("token""write")
// mount -t nfs -o port=1332,mountport=1332,nfsvers=3,noacl,tcp,nolock,intr,rsize=1048576,wsize=1048576,hard,timeo=7,retrans=10,actimeo=10,retry=5 localhost:/47790a91a39dc7f0f8f96cdf0117a4ef mountpoint
// 删除请走auth接口
//
package main
