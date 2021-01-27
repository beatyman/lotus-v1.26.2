// 文档说明
// 本程序利用chattr +a只添加不能删除机制进行zfs存储保护，删除时需要走些程序进行删除操作。
// /check -- 监控zfs磁盘状态，等价于zfs status -x
// /hide?sid=s-t0xxx-xx -- 标记扇区为失败，以便wdpost认为扇区不存在了
// /show?sid=s-t0xxx-xx -- 标记扇区为正常, 以便wdpost继续证明该扇区
// /delete?sid=s-t0xxx-xx -- 删除扇区，注意，该操作不可恢复。
package main
