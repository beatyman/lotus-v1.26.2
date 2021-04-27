-- sqlite3 < v3.25
BEGIN transaction;
update storage_info set mount_auth='d41d8cd98f00b204e9800998ecf8427e' where mount_type='nfs' and kind=0;
update storage_info set mount_opt='2b60d59b5862e7232887de50bc1dddc3' where mount_type='nfs'  and kind=0;

-- only support xx.xx.xx.xx:/data/zfs
update storage_info set mount_auth_uri=substr(mount_signal_uri,1,length(mount_signal_uri)-10)||":1330" where mount_type='nfs';
update storage_info set mount_transf_uri=substr(mount_signal_uri,1,length(mount_signal_uri)-10)||":1331" where mount_type='nfs';
update storage_info set mount_signal_uri=substr(mount_signal_uri,1,length(mount_signal_uri)-10)||":1332" where mount_type='nfs';

update storage_info set mount_type='hlm-storage' where mount_type='nfs' where kind=0;
COMMIT transaction;
