package database

import (
	"fmt"
	"github.com/gwaylib/database"
	"github.com/gwaylib/errors"
	"strings"
	"sync"
	"time"
)

const (
	_MONTH_TABLE_NAME = "MONTH_TABLE_NAME"
)
const (
	tb_worker_sql = `
CREATE TABLE IF NOT EXISTS worker_info (
	id TEXT NOT NULL PRIMARY KEY, /* uuid */
	created_at DATETIME NOT NULL DEFAULT (datetime('now', 'localtime')),
	updated_at DATETIME NOT NULL DEFAULT (datetime('now', 'localtime')),
	ip TEXT NOT NULL DEFAULT '',
	svc_uri TEXT NOT NULL DEFAULT '',
	svc_conn INTEGER NOT NULL DEFAULT 0,
	online INTEGER NOT NULL DEFAULT 0,
	disable INTEGER NOT NULL DEFAULT 0 /* disable should not be allocate*/
);
`
	// for single node storage
	// attention, the rows desigin less than 1000 because should allocate storage will scan full table.
	tb_storage_sql = `
CREATE TABLE IF NOT EXISTS storage_info (
	id INTEGER NOT NULL PRIMARY KEY AUTOINCREMENT,
	created_at DATETIME NOT NULL DEFAULT (datetime('now', 'localtime')),
	updated_at DATETIME NOT NULL DEFAULT (datetime('now', 'localtime')),
	disable INTEGER NOT NULL DEFAULT 0, /* disable should not be allocate for storage. it disgin for local storage, if you have a net storage, should disable it*/
	kind INTEGER NOT NULL DEFAULT 0, /* 0, for sealed; 1 for unsealed. */
	max_size INTEGER NOT NULL, /* max storage size, in byte, filling by manu. */
	keep_size INTEGER DEFAULT 0, /* keep left storage size, in byte, filling by manu. */
	used_size INTEGER DEFAULT 0, /* added by state of secotr success. */
	sector_size INTEGER DEFAULT 107374182400, /* 32GB*3~100GB, using for sum total of used, in byte */
	max_work INTEGER DEFAULT 5, /* limit num of working on precommit and commit. */
	cur_work INTGER DEFAULT 0, /* num of in working, it contains sector_info.state<2, TODO: reset by timeout*/

	mount_type TEXT DEFAULT '', /* mount type, support:nfs,glusterfs,custom,hlm-storage */
	mount_signal_uri TEXT NOT NULL DEFAULT '', /* mount command, like ip:/data/zfs */
	mount_transf_uri TEXT NOT NULL DEFAULT '', /* mount command, like ip:/data/zfs */
	mount_dir TEXT DEFAULT '/data/nfs', /* mount point, will be /data/nfs/id */
	mount_opt TEXT DEFAULT '', /* mount option, seperate by space. */
	mount_auth TEXT NOT NULL DEFAULT '', /* self realization with mount_type.*/
	mount_auth_uri TEXT NOT NULL DEFAULT '', /* server of auth */
	ver BIGINT DEFAULT 1 /* storage item version(time.UnixNano), when update the attribe, should be update this to reload mount.*/
);
CREATE INDEX IF NOT EXISTS sector_info_idx0 ON storage_info(mount_transf_uri);
CREATE INDEX IF NOT EXISTS sector_info_idx1 ON storage_info(mount_auth_uri);
`

	// INSERT INTO storage_info(disable,max_size, max_work,mount_signal_uri, mount_transf_uri, mount_dir)values(1,922372036854775807,1000,'/data/zfs','/data/zfs', '/data/nfs'); /* for default storage in local.*/
	// INSERT INTO storage_info(max_size, max_work,mount_type,mount_signal_uri, mount_transf_uri, mount_dir)values(922372036854775807,5,'nfs','10.1.30.2:/data/zfs', '10.1.30.2:/data/zfs','/data/nfs');
	// INSERT INTO storage_info(max_size, max_work,mount_type,mount_signal_uri, muont_transf_uri, mount_dir)values(922372036854775807,5,'nfs','10.1.30.3:/data/zfs','10.1.30.3:/data/zfs', '/data/nfs');
	// INSERT INTO storage_info(max_size, max_work,mount_type,mount_signal_uri, mount_transf_uri, mount_dir)values(922372036854775807,5,'nfs','10.1.30.4:/data/zfs', '10.1.30.4:/data/zfs','/data/nfs');

	// for every sector
	tb_sector_sql = `
CREATE TABLE IF NOT EXISTS sector_info (
	id TEXT NOT NULL PRIMARY KEY, /* s-t01001-1 */
	created_at DATETIME NOT NULL DEFAULT (datetime('now', 'localtime')),
	updated_at DATETIME NOT NULL DEFAULT (datetime('now', 'localtime')),
	miner_id TEXT NOT NULL, /* t01001 */
	storage_sealed INTEGER DEFAULT 0, /* where the sealed to storage,renamed storage_id to this field */
	storage_unsealed INTEGER DEFAULT 0, /* where the market unsealed data to storage */
	worker_id TEXT NOT NULL DEFAULT 'default', /* who work on */
	state INTEGER NOT NULL DEFAULT 0, /* 0: INIT, 0-99:working, 100:moving, 1001:pushing, 200: success, 500: failed.*/
	state_time DATETIME NOT NULL DEFAULT (datetime('now', 'localtime')), /* state update time, design for state timeout.*/
	state_msg TEXT NOT NULL DEFAULT '', /* msg for state */
	state_times INT NOT NULL DEFAULT 0 /* count the state change event times, cause the sealing should be ran in a loop. */
);
CREATE INDEX IF NOT EXISTS sector_info_idx0 ON sector_info(worker_id,state);
CREATE INDEX IF NOT EXISTS sector_info_idx1 ON sector_info(miner_id);
CREATE INDEX IF NOT EXISTS sector_info_idx2 ON sector_info(state,state_time);
CREATE INDEX IF NOT EXISTS sector_info_idx3 ON sector_info(storage_sealed,state);
CREATE INDEX IF NOT EXISTS sector_info_idx4 ON sector_info(storage_unsealed,state);
`
	// for snap upgrade
	tb_sector_snap_sql = `alter table sector_info add snap integer default 0 not null;`

	tb_sector_rebuild_sql = `
CREATE TABLE IF NOT EXISTS sector_rebuild (
	id TEXT NOT NULL PRIMARY KEY, /* s-t01001-1 */
	created_at DATETIME NOT NULL DEFAULT (datetime('now', 'localtime')),
	updated_at DATETIME NOT NULL DEFAULT (datetime('now', 'localtime')),
	storage_sealed INTEGER DEFAULT 0
);
`

	tb_market_sql = `
CREATE TABLE IF NOT EXISTS market_retrieve (
	sid TEXT NOT NULL PRIMARY KEY, /* s-t01001-1 */
	created_at DATETIME NOT NULL DEFAULT (datetime('now', 'localtime')),
	updated_at DATETIME NOT NULL DEFAULT (datetime('now', 'localtime')),
	retrieve_times INTEGER NOT NULL DEFAULT 1, 
	retrieve_time DATETIME NOT NULL DEFAULT (datetime('now', 'localtime')),
	active INT DEFAULT 1
);
CREATE INDEX IF NOT EXISTS market_retrieve_idx0 ON market_retrieve(retrieve_time,active);
`
	tb_statis_win_sql = `
CREATE TABLE IF NOT EXISTS statis_win (
	id TEXT NOT NULL PRIMARY KEY, /*20210517*/
	created_at DATETIME NOT NULL DEFAULT (datetime('now', 'localtime')),
	updated_at DATETIME NOT NULL DEFAULT (datetime('now', 'localtime')),
	win_all INTEGER NOT NULL DEFAULT 0, /*触发调用mineOne的次数，正常运行的情况下等于轮数*/
	win_err INTEGER NOT NULL DEFAULT 0, /*调用mineOne不成功的次数*/
	win_gen INTEGER NOT NULL DEFAULT 0, /*本地中奖的次数*/
	win_suc INTEGER NOT NULL DEFAULT 0, /*成功成为TipSet*/
	win_exp INTEGER NOT NULL DEFAULT 0, /*预期可中奖次数，等于天总轮数(固定值)乘以概率值*/
	win_used INTEGER NOT NULL DEFAULT 0 /*本地计算中奖的用时，单位为毫秒，除以win_gen可得到平均用时*/
);
`
	tb_statis_seal_headname = "statis_seal"
	tb_statis_seal_sql      = `
CREATE TABLE IF NOT EXISTS MONTH_TABLE_NAME (
	task_id TEXT NOT NULL PRIMARY KEY,
	created_at DATETIME NOT NULL DEFAULT (datetime('now', 'localtime')),
	updated_at DATETIME NOT NULL DEFAULT (datetime('now', 'localtime')),

	sid TEXT NOT NULL,
	stage TEXT NOT NULL,
	worker_id TEXT NOT NULL DEFAULT '',
	begin_time DATETIME NOT NULL DEFAULT (datetime('now', 'localtime')),
	end_time DATETIME NOT NULL DEFAULT (datetime('now', 'localtime')),
	used INTEGER NOT NULL DEFAULT 0, /* ns */

	error TEXT NOT NULL DEFAULT '', /* success, other's failed message. */
	is_rsync INTEGER NOT NULL DEFAULT 0 /* 0:no,1:yes */

);
CREATE INDEX IF NOT EXISTS MONTH_TABLE_NAME_idx0 ON MONTH_TABLE_NAME(sid);
CREATE INDEX IF NOT EXISTS MONTH_TABLE_NAME_idx1 ON MONTH_TABLE_NAME(created_at,worker_id);
CREATE INDEX IF NOT EXISTS MONTH_TABLE_NAME_idx2 ON MONTH_TABLE_NAME(begin_time,stage);
`
	tb_market_deal_sql = `
CREATE TABLE IF NOT EXISTS market_deal (
	id TEXT NOT NULL PRIMARY KEY, /* propid */
	created_at DATETIME NOT NULL DEFAULT (datetime('now', 'localtime')),
	updated_at DATETIME NOT NULL DEFAULT (datetime('now', 'localtime')),
	root_cid TEXT NOT NULL DEFAULT '',
	piece_cid TEXT NOT NULL DEFAULT '',
	piece_size BIGINT NOT NULL DEFAULT 0, /* unppded?*/
	client_addr TEXT NOT NULL DEFAULT '',
	file_local TEXT NOT NULL DEFAULT '',
	file_remote TEXT NOT NULL DEFAULT '',
	file_storage INT NOT NULL DEFAULT 0,
	
	sid TEXT NOT NULL DEFAULT '', /* maybe not allocated, and maybe several deals for one sector. */
	deal_num BIGINT NOT NULL DEFAULT -1, /* a deal id num in sector sealing. */
	offset BIGINT NOT NULL DEFAULT 0,
	state INT NOT NULL DEFAULT 0 /* same as sector_info */
);
CREATE INDEX IF NOT EXISTS market_deal_idx0 ON market_deal(sid);
CREATE INDEX IF NOT EXISTS market_deal_idx1 ON market_deal(created_at);
CREATE INDEX IF NOT EXISTS market_deal_idx2 ON market_deal(file_local);
`
)

var (
	monthTbNameCache = map[string]string{}
	monthTbLock      = sync.Mutex{}
)

func getMonthTableName(tableNameHead string, date time.Time) string {
	timefmt := date.Format("200601")
	return fmt.Sprintf(tableNameHead + "_" + timefmt)
}

// -- create sql need contain _MONTH_TABLE_NAME
func createMonthTable(exec database.Execer, tableNameHead, createSql string, currentTime time.Time) (string, error) {
	monthTbLock.Lock()
	defer monthTbLock.Unlock()

	tmpTbName := getMonthTableName(tableNameHead, currentTime)

	// read from cache
	tbName, ok := monthTbNameCache[tableNameHead]
	if ok && tmpTbName == tbName {
		return tbName, nil
	}

	tbName = tmpTbName
	// create a new table to storage the log
	destSql := strings.Replace(createSql, _MONTH_TABLE_NAME, tbName, -1)
	if _, err := exec.Exec(destSql); err != nil {
		return "", errors.As(err)
	}
	// put in cache when created successfull
	monthTbNameCache[tableNameHead] = tbName
	return tbName, nil
}
