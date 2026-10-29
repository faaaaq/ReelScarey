CREATE TABLE activity_events (
 id INTEGER PRIMARY KEY,
 occurred_at INTEGER NOT NULL,
 kind TEXT NOT NULL,
 object_id TEXT NOT NULL,
 title TEXT NOT NULL,
 state TEXT NOT NULL
);
CREATE INDEX activity_events_time ON activity_events(id DESC);
CREATE TRIGGER event_media_added AFTER INSERT ON media_items BEGIN
 INSERT INTO activity_events(occurred_at,kind,object_id,title,state) VALUES(unixepoch(),'title',NEW.id,NEW.title,'added');
END;
CREATE TRIGGER event_media_monitoring AFTER UPDATE OF monitored ON media_items WHEN OLD.monitored<>NEW.monitored BEGIN
 INSERT INTO activity_events(occurred_at,kind,object_id,title,state) VALUES(unixepoch(),'title',NEW.id,NEW.title,CASE WHEN NEW.monitored=1 THEN 'monitoring enabled' ELSE 'monitoring paused' END);
END;
CREATE TRIGGER event_media_file_added AFTER INSERT ON media_files BEGIN
 INSERT INTO activity_events(occurred_at,kind,object_id,title,state)
 SELECT unixepoch(),'title',m.id,m.title,'file registered' FROM media_items m WHERE m.id=COALESCE(NEW.movie_id,NEW.series_id);
END;
CREATE TRIGGER event_download_added AFTER INSERT ON downloads BEGIN
 INSERT INTO activity_events(occurred_at,kind,object_id,title,state) VALUES(unixepoch(),'download',NEW.id,NEW.title,NEW.state);
END;
CREATE TRIGGER event_download_changed AFTER UPDATE OF state ON downloads WHEN OLD.state<>NEW.state BEGIN
 INSERT INTO activity_events(occurred_at,kind,object_id,title,state) VALUES(unixepoch(),'download',NEW.id,NEW.title,NEW.state);
END;
CREATE TRIGGER event_youtube_added AFTER INSERT ON youtube_jobs BEGIN
 INSERT INTO activity_events(occurred_at,kind,object_id,title,state) VALUES(unixepoch(),'youtube',NEW.id,NEW.video_title,NEW.state);
END;
CREATE TRIGGER event_youtube_changed AFTER UPDATE OF state ON youtube_jobs WHEN OLD.state<>NEW.state BEGIN
 INSERT INTO activity_events(occurred_at,kind,object_id,title,state) VALUES(unixepoch(),'youtube',NEW.id,NEW.video_title,NEW.state);
END;
CREATE TRIGGER event_request_added AFTER INSERT ON media_requests BEGIN
 INSERT INTO activity_events(occurred_at,kind,object_id,title,state) VALUES(unixepoch(),'request',NEW.id,NEW.title,NEW.state);
END;
CREATE TRIGGER event_request_changed AFTER UPDATE OF state ON media_requests WHEN OLD.state<>NEW.state BEGIN
 INSERT INTO activity_events(occurred_at,kind,object_id,title,state) VALUES(unixepoch(),'request',NEW.id,NEW.title,NEW.state);
END;
CREATE TRIGGER event_request_notice_changed AFTER UPDATE OF state ON request_notifications WHEN OLD.state<>NEW.state AND NEW.state IN ('sent','uncertain') BEGIN
 INSERT INTO activity_events(occurred_at,kind,object_id,title,state)
 SELECT unixepoch(),'request',m.id,m.title,CASE WHEN NEW.state='sent' THEN 'notification sent' ELSE 'notification uncertain' END FROM media_requests m WHERE m.id=NEW.request_id;
END;
CREATE TRIGGER event_organization_added AFTER INSERT ON organization_plans BEGIN
 INSERT INTO activity_events(occurred_at,kind,object_id,title,state) VALUES(unixepoch(),'organization',NEW.id,'Library organization',NEW.state);
END;
CREATE TRIGGER event_organization_changed AFTER UPDATE OF state ON organization_plans WHEN OLD.state<>NEW.state BEGIN
 INSERT INTO activity_events(occurred_at,kind,object_id,title,state) VALUES(unixepoch(),'organization',NEW.id,'Library organization',NEW.state);
END;
CREATE TRIGGER event_subtitle_added AFTER INSERT ON subtitle_jobs BEGIN
 INSERT INTO activity_events(occurred_at,kind,object_id,title,state) VALUES(unixepoch(),'subtitle',NEW.id,'Subtitle job',NEW.state);
END;
CREATE TRIGGER event_subtitle_changed AFTER UPDATE OF state ON subtitle_jobs WHEN OLD.state<>NEW.state BEGIN
 INSERT INTO activity_events(occurred_at,kind,object_id,title,state) VALUES(unixepoch(),'subtitle',NEW.id,'Subtitle job',NEW.state);
END;
CREATE TRIGGER event_play_added AFTER INSERT ON emby_play_history BEGIN
 INSERT INTO activity_events(occurred_at,kind,object_id,title,state) VALUES(unixepoch(),'playback',NEW.id,NEW.title,NEW.state);
END;
CREATE TRIGGER event_play_changed AFTER UPDATE OF state ON emby_play_history WHEN OLD.state<>NEW.state BEGIN
 INSERT INTO activity_events(occurred_at,kind,object_id,title,state) VALUES(unixepoch(),'playback',NEW.id,NEW.title,NEW.state);
END;
CREATE TRIGGER event_play_pause AFTER UPDATE OF paused ON emby_play_history WHEN OLD.paused<>NEW.paused AND NEW.state='active' BEGIN
 INSERT INTO activity_events(occurred_at,kind,object_id,title,state) VALUES(unixepoch(),'playback',NEW.id,NEW.title,CASE WHEN NEW.paused=1 THEN 'paused' ELSE 'resumed' END);
END;
CREATE TRIGGER event_automation_added AFTER INSERT ON automation_runs BEGIN
 INSERT INTO activity_events(occurred_at,kind,object_id,title,state) VALUES(unixepoch(),'automation',NEW.id,'Scheduled search',NEW.state);
END;
CREATE TRIGGER event_automation_changed AFTER UPDATE OF state ON automation_runs WHEN OLD.state<>NEW.state BEGIN
 INSERT INTO activity_events(occurred_at,kind,object_id,title,state) VALUES(unixepoch(),'automation',NEW.id,'Scheduled search',NEW.state);
END;
CREATE TRIGGER event_scan_added AFTER INSERT ON library_scans BEGIN
 INSERT INTO activity_events(occurred_at,kind,object_id,title,state) VALUES(unixepoch(),'scan',NEW.id,'Library scan',NEW.state);
END;
CREATE TRIGGER event_scan_changed AFTER UPDATE OF state ON library_scans WHEN OLD.state<>NEW.state BEGIN
 INSERT INTO activity_events(occurred_at,kind,object_id,title,state) VALUES(unixepoch(),'scan',NEW.id,'Library scan',NEW.state);
END;
CREATE TRIGGER event_scan_file_changed AFTER UPDATE OF state ON library_scan_files WHEN OLD.state<>NEW.state AND NEW.state IN ('registered','blocked') BEGIN
 INSERT INTO activity_events(occurred_at,kind,object_id,title,state) VALUES(unixepoch(),'scan',NEW.scan_id,NEW.relative_path,NEW.state);
END;
