-- sqlite3 < v3.25
BEGIN TRANSACTION;

alter table storage_info add column kind INTEGER NOT NULL default 0;


-- rebuild sector_info cause sqlite can't rename the field.
ALTER TABLE sector_info RENAME TO sector_info_old;
CREATE TABLE IF NOT EXISTS sector_info (
	id TEXT NOT NULL PRIMARY KEY, /* s-t0101-1 */
	created_at DATETIME NOT NULL DEFAULT (datetime('now', 'localtime')),
	updated_at DATETIME NOT NULL DEFAULT (datetime('now', 'localtime')),
	miner_id TEXT NOT NULL, /* t0101 */
	storage_sealed INTEGER DEFAULT 0, /* where the sealed to storage,renamed storage_id to this field */
	storage_unsealed INTEGER DEFAULT 0, /* where the market unsealed data to storage */
	worker_id TEXT NOT NULL DEFAULT 'default', /* who work on */
	state INTEGER NOT NULL DEFAULT 0, /* 0: INIT, 0-99:working, 100:moving, 101:pushing, 200: success, 500: failed.*/
	state_time DATETIME NOT NULL DEFAULT (datetime('now', 'localtime')), /* state update time, design for state timeout.*/
	state_msg TEXT NOT NULL DEFAULT '', /* msg for state */
	state_times INT NOT NULL DEFAULT 0 /* count the state change event times, cause the sealing should be ran in a loop. */
);
CREATE INDEX IF NOT EXISTS sector_info_idx0 ON sector_info(worker_id);
CREATE INDEX IF NOT EXISTS sector_info_idx1 ON sector_info(miner_id);
CREATE INDEX IF NOT EXISTS sector_info_idx2 ON sector_info(state,state_time);
CREATE INDEX IF NOT EXISTS sector_info_idx3 ON sector_info(storage_sealed);
CREATE INDEX IF NOT EXISTS sector_info_idx4 ON sector_info(storage_unsealed);

INSERT INTO sector_info (id,created_at,updated_at,miner_id,storage_sealed,storage_unsealed,worker_id,state,state_time,state_msg,state_times) SELECT id,created_at,updated_at,miner_id,storage_id,storage_unsealed,worker_id,state,state_time,state_msg,state_times FROM sector_info_old;
COMMIT;

-- sqlite >= v3.25
/*
BEGIN TRANSACTION;
alter table storage_info add column kind INTEGER NOT NULL default 0;
alter table sector_info add column storage_unsealed INTEGER NOT NULL default 0;
alter table sector_info rename column storage_id to storage_sealed;
CREATE INDEX IF NOT EXISTS sector_info_idx4 ON sector_info(storage_unsealed);
*/
