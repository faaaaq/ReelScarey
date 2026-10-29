CREATE TABLE request_lists_next (
    id INTEGER PRIMARY KEY,
    account_id INTEGER NOT NULL REFERENCES request_accounts(id) ON DELETE CASCADE,
    provider TEXT NOT NULL CHECK(provider IN ('tmdb','trakt','imdb_mdblist','tmdb_private','trakt_private')),
    owner TEXT NOT NULL DEFAULT '',
    list_id TEXT NOT NULL,
    ciphertext BLOB,
    enabled INTEGER NOT NULL DEFAULT 0 CHECK(enabled IN (0,1)),
    revision INTEGER NOT NULL DEFAULT 1,
    last_error TEXT NOT NULL DEFAULT '',
    last_checked INTEGER NOT NULL DEFAULT 0,
    next_check INTEGER NOT NULL DEFAULT 0,
    lease_until INTEGER NOT NULL DEFAULT 0,
    UNIQUE(account_id,provider,owner,list_id)
);
INSERT INTO request_lists_next SELECT * FROM request_lists;
DROP TABLE request_lists;
ALTER TABLE request_lists_next RENAME TO request_lists;

CREATE TRIGGER event_request_list_added AFTER INSERT ON request_lists BEGIN
 INSERT INTO activity_events(occurred_at,kind,object_id,title,state) VALUES(unixepoch(),'request_list',NEW.id,NEW.provider,'import list added');
END;
CREATE TRIGGER event_request_list_changed AFTER UPDATE OF revision ON request_lists WHEN OLD.revision<>NEW.revision BEGIN
 INSERT INTO activity_events(occurred_at,kind,object_id,title,state) VALUES(unixepoch(),'request_list',NEW.id,NEW.provider,'import list updated');
END;
CREATE TRIGGER event_request_list_removed AFTER DELETE ON request_lists BEGIN
 INSERT INTO activity_events(occurred_at,kind,object_id,title,state) VALUES(unixepoch(),'request_list',OLD.id,OLD.provider,'import list removed');
END;
