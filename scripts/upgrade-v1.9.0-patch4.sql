-- sqlite3 < v3.25
BEGIN transaction;
alter table statis_win add column win_exp INTEGER NOT NULL default 0;
alter table statis_win add column win_used INTEGER NOT NULL default 0;
COMMIT transaction;
