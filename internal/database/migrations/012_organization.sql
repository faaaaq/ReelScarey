CREATE TABLE organization_config (
 id INTEGER PRIMARY KEY CHECK(id=1),
 revision INTEGER NOT NULL DEFAULT 1,
 libraries TEXT NOT NULL DEFAULT '[]'
);
INSERT INTO organization_config(id) VALUES(1);
CREATE TABLE media_organization (
 media_id INTEGER PRIMARY KEY REFERENCES media_items(id),
 library_key TEXT NOT NULL,
 revision INTEGER NOT NULL DEFAULT 1
);
ALTER TABLE downloads ADD COLUMN organization_snapshot TEXT NOT NULL DEFAULT '';
ALTER TABLE download_targets ADD COLUMN absolute_number INTEGER;
CREATE TABLE organization_plans (
 id TEXT PRIMARY KEY,
 state TEXT NOT NULL CHECK(state IN ('review','queued','running','failed','completed')),
 message TEXT NOT NULL DEFAULT '',
 snapshot TEXT NOT NULL,
 created_at INTEGER NOT NULL,
 updated_at INTEGER NOT NULL
);
CREATE UNIQUE INDEX organization_one_work ON organization_plans((1)) WHERE state IN ('queued','running');
CREATE TABLE organization_outputs (
 plan_id TEXT NOT NULL REFERENCES organization_plans(id),
 destination TEXT NOT NULL,
 stage TEXT NOT NULL,
 identity TEXT NOT NULL DEFAULT '',
 phase TEXT NOT NULL DEFAULT 'planned',
 PRIMARY KEY(plan_id,destination)
);
