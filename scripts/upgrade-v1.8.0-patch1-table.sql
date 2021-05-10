BEGIN transaction;
alter table storage_info add column mount_auth TEXT NOT NULL DEFAULT '';
alter table storage_info add column mount_auth_uri TEXT NOT NULL DEFAULT '';
CREATE INDEX IF NOT EXISTS sector_info_idx1 ON storage_info(mount_auth_uri);
COMMIT transaction;
