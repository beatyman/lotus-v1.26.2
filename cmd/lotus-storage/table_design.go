package main

const (
	tb_nfs_session_sql = `
CREATE TABLE IF NOT EXISTS nfs_session (
	id TEXT NOT NULL PRIMARY KEY,
	created_at DATETIME NOT NULL DEFAULT (datetime('now', 'localtime')),
	updated_at DATETIME NOT NULL DEFAULT (datetime('now', 'localtime')),

	auth TEXT NOT NULL DEFAULT '', /* server of auth */
	path TEXT NOT NULL DEFAULT '' /* session path. */
);
`
)
