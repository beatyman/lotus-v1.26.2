package main

const (
	tb_file_session_sql = `
CREATE TABLE IF NOT EXISTS file_session (
	id TEXT NOT NULL PRIMARY KEY,
	created_at DATETIME NOT NULL DEFAULT (datetime('now', 'localtime')),
	updated_at DATETIME NOT NULL DEFAULT (datetime('now', 'localtime')),

	user TEXT NOT NULL DEFAULT '', /* auth scope */
	auth TEXT NOT NULL DEFAULT '', /* server of auth */
	path TEXT NOT NULL DEFAULT '' /* session path. */
);
`
)
