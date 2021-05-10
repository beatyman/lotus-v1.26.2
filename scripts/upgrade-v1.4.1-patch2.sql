-- sqlite3 < v3.25
BEGIN transaction;
DROP INDEX sector_info_idx0;
DROP INDEX sector_info_idx1;
DROP INDEX sector_info_idx2;
DROP INDEX sector_info_idx3;
DROP INDEX sector_info_idx4;
CREATE INDEX IF NOT EXISTS sector_info_idx0 ON sector_info(worker_id,state);
CREATE INDEX IF NOT EXISTS sector_info_idx1 ON sector_info(miner_id);
CREATE INDEX IF NOT EXISTS sector_info_idx2 ON sector_info(state,state_time);
CREATE INDEX IF NOT EXISTS sector_info_idx3 ON sector_info(storage_sealed,state);
CREATE INDEX IF NOT EXISTS sector_info_idx4 ON sector_info(storage_unsealed,state);
COMMIT transaction;

