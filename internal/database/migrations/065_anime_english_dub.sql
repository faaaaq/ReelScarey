ALTER TABLE anime_search_settings ADD COLUMN english_dub_only INTEGER NOT NULL DEFAULT 0 CHECK(english_dub_only IN (0,1));

CREATE TABLE anime_preferences (
    id INTEGER PRIMARY KEY CHECK(id=1),
    dub_rule TEXT NOT NULL,
    revision INTEGER NOT NULL DEFAULT 1 CHECK(revision>0),
    updated_at INTEGER NOT NULL
);

INSERT INTO anime_preferences(id,dub_rule,revision,updated_at) VALUES(1,'/([\[\(][Ee]nglish[\W]?([aA-zZ ]+)?[Dd]ubs?|FuniDub|Dual( |-|.)?Audio|\[Dual\])/i
/([\[\(][Ee]ng[\W]?([aA-zZ ]+)?[Dd]ubs?|FuniDub|Dual( |-|.)?Audio|\[Dual\])/i
Eng Dub
Eng Dubs
English Dub
English Dubs
dual-audio
Dual-Audio',1,unixepoch());
