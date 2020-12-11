alter table storage_info add column kind INTEGER NOT NULL default 0;
alter table sector_info add column storage_unsealed INTEGER default 0;
