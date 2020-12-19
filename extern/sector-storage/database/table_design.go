package database

const (
	tb_worker_sql = `
CREATE TABLE IF NOT EXISTS worker_info (
	id TEXT NOT NULL PRIMARY KEY, /* uuid */
	created_at DATETIME NOT NULL DEFAULT (datetime('now', 'localtime')),
	updated_at DATETIME NOT NULL DEFAULT (datetime('now', 'localtime')),
	ip TEXT NOT NULL DEFAULT '',
	svc_uri TEXT NOT NULL DEFAULT '',
	svc_conn INTERGER NOT NULL DEFAULT 0,
	online INTEGER NOT NULL DEFAULT 0,
	disable INTERGER NOT NULL DEFAULT 0 /* disable should not be allocate*/
);
`
	// for single node storage
	// attention, the rows desigin less than 1000 because should allocate storage will scan full table.
	tb_storage_sql = `
CREATE TABLE IF NOT EXISTS storage_info (
	id INTEGER NOT NULL PRIMARY KEY AUTOINCREMENT,
	created_at DATETIME NOT NULL DEFAULT (datetime('now', 'localtime')),
	updated_at DATETIME NOT NULL DEFAULT (datetime('now', 'localtime')),
	disable INTERGER NOT NULL DEFAULT 0, /* disable should not be allocate for storage. it disgin for local storage, if you have a net storage, should disable it*/
	kind INTERGER NOT NULL DEFAULT 0, /* 0, for sealed; 1 for unsealed. */
	max_size INTEGER NOT NULL, /* max storage size, in byte, filling by manu. */
	keep_size INTEGER DEFAULT 0, /* keep left storage size, in byte, filling by manu. */
	used_size INTEGER DEFAULT 0, /* added by state of secotr success. */
	sector_size INTEGER DEFAULT 107374182400, /* 32GB*3~100GB, using for sum total of used, in byte */
	max_work INTEGER DEFAULT 5, /* limit num of working on precommit and commit. */
	cur_work INTGER DEFAULT 0, /* num of in working, it contains sector_info.state<2, TODO: reset by timeout*/
	mount_type TEXT DEFAULT '', /* mount type, empty for no mount */
	mount_signal_uri TEXT NOT NULL DEFAULT '', /* mount command, like ip:/data/zfs */
	mount_transf_uri TEXT NOT NULL DEFAULT '', /* mount command, like ip:/data/zfs */
	mount_dir TEXT DEFAULT '/data/nfs', /* mount point, will be /data/nfs/id */
	mount_opt TEXT DEFAULT '', /* mount option, seperate by space. */
	ver BIGINT DEFAULT 1 /* storage item version(time.UnixNano), when update the attribe, should be update this to reload mount.*/
);
CREATE INDEX IF NOT EXISTS sector_info_idx0 ON storage_info(mount_transf_uri);
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
CREATE INDEX IF NOT EXISTS sector_info_idx0 ON sector_info(worker_id);
CREATE INDEX IF NOT EXISTS sector_info_idx1 ON sector_info(miner_id);
CREATE INDEX IF NOT EXISTS sector_info_idx2 ON sector_info(state,state_time);
CREATE INDEX IF NOT EXISTS sector_info_idx3 ON sector_info(storage_sealed);
CREATE INDEX IF NOT EXISTS sector_info_idx4 ON sector_info(storage_unsealed);
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
)
