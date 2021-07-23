-- sqlite3 < v3.25
BEGIN transaction;
update storage_info set mount_auth='d41d8cd98f00b204e9800998ecf8427e' where mount_type='nfs' and kind=0;
update storage_info set mount_opt='2b60d59b5862e7232887de50bc1dddc3' where mount_type='nfs'  and kind=0;

-- only support xx.xx.xx.xx:/data/zfs
update storage_info set mount_auth_uri=substr(mount_signal_uri,1,length(mount_signal_uri)-10)||":1330" where mount_type='nfs' and kind=0 and substr(mount_signal_uri,length(mount_signal_uri)-9, 10)=':/data/zfs';
update storage_info set mount_transf_uri=substr(mount_signal_uri,1,length(mount_signal_uri)-10)||":1331" where mount_type='nfs' and kind=0 and substr(mount_signal_uri,length(mount_signal_uri)-9, 10)=':/data/zfs';
-- last update the mount_signal_uri
update storage_info set mount_signal_uri=substr(mount_signal_uri,1,length(mount_signal_uri)-5)||":/data/zfs" where mount_type='hlm-storage' and kind=0 and substr(mount_signal_uri,length(mount_signal_uri)-4, 5)=':1332';
update storage_info set mount_signal_uri=substr(mount_signal_uri,1,length(mount_signal_uri)-5)||":/data/zfs1" where mount_type='hlm-storage' and kind=0 and substr(mount_signal_uri,length(mount_signal_uri)-4, 5)=':1342';
update storage_info set mount_signal_uri=substr(mount_signal_uri,1,length(mount_signal_uri)-5)||":/data/zfs2" where mount_type='hlm-storage' and kind=0 and substr(mount_signal_uri,length(mount_signal_uri)-4, 5)=':1352';

update storage_info set mount_type='hlm-storage' where mount_type='nfs' and kind=0;
COMMIT transaction;