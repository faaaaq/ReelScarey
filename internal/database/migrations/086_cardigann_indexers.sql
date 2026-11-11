-- Add the Cardigann definition-backed indexer type. The rebuild preserves every
-- saved row and recreates the secret-free indexer activity triggers.
CREATE TABLE indexers_next (
 id TEXT PRIMARY KEY,
 name TEXT NOT NULL CHECK(length(name) BETWEEN 1 AND 80),
 kind TEXT NOT NULL CHECK(kind IN ('torznab','newznab','iptorrents','xspeeds','nyaa','rss','cardigann')),
 enabled INTEGER NOT NULL CHECK(enabled IN (0,1)),
 revision INTEGER NOT NULL DEFAULT 1,
 generation TEXT NOT NULL,
 ciphertext BLOB NOT NULL,
 updated_at INTEGER NOT NULL,
 last_test_at INTEGER NOT NULL DEFAULT 0,
 last_search_at INTEGER NOT NULL DEFAULT 0,
 last_error TEXT NOT NULL DEFAULT '',
 last_result_count INTEGER NOT NULL DEFAULT 0
);
INSERT INTO indexers_next(id,name,kind,enabled,revision,generation,ciphertext,updated_at,last_test_at,last_search_at,last_error,last_result_count)
 SELECT id,name,kind,enabled,revision,generation,ciphertext,updated_at,last_test_at,last_search_at,last_error,last_result_count FROM indexers;
DROP TABLE indexers;
ALTER TABLE indexers_next RENAME TO indexers;
CREATE TRIGGER event_indexer_added AFTER INSERT ON indexers BEGIN
  INSERT INTO activity_events(occurred_at,kind,object_id,title,state)
  VALUES(unixepoch(),'indexer',NEW.id,NEW.name,'indexer added');
END;
CREATE TRIGGER event_indexer_changed AFTER UPDATE OF revision ON indexers WHEN OLD.revision<>NEW.revision BEGIN
  INSERT INTO activity_events(occurred_at,kind,object_id,title,state)
  VALUES(unixepoch(),'indexer',NEW.id,NEW.name,'indexer updated');
END;
CREATE TRIGGER event_indexer_removed AFTER DELETE ON indexers BEGIN
  INSERT INTO activity_events(occurred_at,kind,object_id,title,state)
  VALUES(unixepoch(),'indexer',OLD.id,OLD.name,'indexer removed');
END;
