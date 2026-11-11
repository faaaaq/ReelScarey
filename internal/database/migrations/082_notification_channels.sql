-- Administrator notification channels. Provider secrets are sealed with the
-- instance key; config_json holds only non-secret routing fields.
CREATE TABLE notification_channels (
    id INTEGER PRIMARY KEY,
    name TEXT NOT NULL CHECK(length(name) BETWEEN 1 AND 80),
    kind TEXT NOT NULL CHECK(kind IN ('email','ntfy','gotify','pushover','telegram','discord','webhook')),
    enabled INTEGER NOT NULL DEFAULT 1 CHECK(enabled IN (0,1)),
    config_json TEXT NOT NULL DEFAULT '{}' CHECK(length(config_json)<=16384),
    events_json TEXT NOT NULL DEFAULT '[]' CHECK(length(events_json)<=4096),
    secret_ciphertext BLOB,
    revision INTEGER NOT NULL DEFAULT 1,
    created_at INTEGER NOT NULL,
    updated_at INTEGER NOT NULL
);
CREATE TABLE notification_outbox (
    id INTEGER PRIMARY KEY,
    channel_id INTEGER NOT NULL REFERENCES notification_channels(id) ON DELETE CASCADE,
    event TEXT NOT NULL CHECK(length(event) BETWEEN 1 AND 80),
    title TEXT NOT NULL CHECK(length(title)<=200),
    body TEXT NOT NULL CHECK(length(body)<=2000),
    state TEXT NOT NULL CHECK(state IN ('pending','sending','sent','failed','uncertain','cancelled')),
    attempts INTEGER NOT NULL DEFAULT 0,
    next_attempt INTEGER NOT NULL DEFAULT 0,
    last_error TEXT NOT NULL DEFAULT '',
    created_at INTEGER NOT NULL,
    updated_at INTEGER NOT NULL
);
CREATE INDEX notification_outbox_due ON notification_outbox(state,next_attempt);
CREATE TABLE notification_state (
    id INTEGER PRIMARY KEY CHECK(id=1),
    last_event_id INTEGER NOT NULL DEFAULT 0
);
INSERT INTO notification_state(id,last_event_id) VALUES(1, COALESCE((SELECT MAX(id) FROM activity_events),0));
