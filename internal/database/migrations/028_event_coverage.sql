-- Only recorded creation times and the last known state can be recovered.
-- Missing intermediate transitions are not invented.
CREATE INDEX activity_events_object ON activity_events(kind,object_id,state,occurred_at);
INSERT INTO activity_events(occurred_at,kind,object_id,title,state)
 SELECT CAST(strftime('%s',m.created_at) AS INTEGER),'title',m.id,m.title,'added'
 FROM media_items m WHERE strftime('%s',m.created_at) IS NOT NULL
 AND NOT EXISTS(SELECT 1 FROM activity_events e WHERE e.kind='title' AND e.object_id=CAST(m.id AS TEXT) AND e.state='added');
INSERT INTO activity_events(occurred_at,kind,object_id,title,state)
 SELECT d.updated_at,'download',d.id,d.title,'historical state: ' || d.state FROM downloads d
 WHERE d.updated_at>0 AND NOT EXISTS(SELECT 1 FROM activity_events e WHERE e.kind='download' AND e.object_id=d.id);
INSERT INTO activity_events(occurred_at,kind,object_id,title,state)
 SELECT d.created_at,'download',d.id,d.title,'historical record created' FROM downloads d
 WHERE d.created_at>0 AND NOT EXISTS(SELECT 1 FROM activity_events e WHERE e.kind='download' AND e.object_id=d.id AND e.occurred_at=d.created_at);
INSERT INTO activity_events(occurred_at,kind,object_id,title,state)
 SELECT r.updated_at,'request',r.id,r.title,'historical state: ' || r.state FROM media_requests r
 WHERE r.updated_at>0 AND NOT EXISTS(SELECT 1 FROM activity_events e WHERE e.kind='request' AND e.object_id=CAST(r.id AS TEXT));
INSERT INTO activity_events(occurred_at,kind,object_id,title,state)
 SELECT r.created_at,'request',r.id,r.title,'historical record created' FROM media_requests r
 WHERE r.created_at>0 AND NOT EXISTS(SELECT 1 FROM activity_events e WHERE e.kind='request' AND e.object_id=CAST(r.id AS TEXT) AND e.occurred_at=r.created_at);
INSERT INTO activity_events(occurred_at,kind,object_id,title,state)
 SELECT y.updated_at,'youtube',y.id,y.video_title,'historical state: ' || y.state FROM youtube_jobs y
 WHERE y.updated_at>0 AND NOT EXISTS(SELECT 1 FROM activity_events e WHERE e.kind='youtube' AND e.object_id=y.id);
INSERT INTO activity_events(occurred_at,kind,object_id,title,state)
 SELECT y.created_at,'youtube',y.id,y.video_title,'historical record created' FROM youtube_jobs y
 WHERE y.created_at>0 AND NOT EXISTS(SELECT 1 FROM activity_events e WHERE e.kind='youtube' AND e.object_id=y.id AND e.occurred_at=y.created_at);
INSERT INTO activity_events(occurred_at,kind,object_id,title,state)
 SELECT a.started_at,'automation',a.id,'Scheduled search','historical state: ' || a.state FROM automation_runs a
 WHERE a.started_at>0 AND NOT EXISTS(SELECT 1 FROM activity_events e WHERE e.kind='automation' AND e.object_id=a.id);
INSERT INTO activity_events(occurred_at,kind,object_id,title,state)
 SELECT o.updated_at,'organization',o.id,'Library organization','historical state: ' || o.state FROM organization_plans o
 WHERE o.updated_at>0 AND NOT EXISTS(SELECT 1 FROM activity_events e WHERE e.kind='organization' AND e.object_id=o.id);
INSERT INTO activity_events(occurred_at,kind,object_id,title,state)
 SELECT s.updated_at,'subtitle',s.id,'Subtitle job','historical state: ' || s.state FROM subtitle_jobs s
 WHERE s.updated_at>0 AND NOT EXISTS(SELECT 1 FROM activity_events e WHERE e.kind='subtitle' AND e.object_id=s.id);
INSERT INTO activity_events(occurred_at,kind,object_id,title,state)
 SELECT s.updated_at,'scan',s.id,'Library scan','historical state: ' || s.state FROM library_scans s
 WHERE s.updated_at>0 AND NOT EXISTS(SELECT 1 FROM activity_events e WHERE e.kind='scan' AND e.object_id=s.id);
CREATE INDEX activity_events_chronological ON activity_events(occurred_at DESC,id DESC);

CREATE TRIGGER event_related_added AFTER INSERT ON related_titles BEGIN
 INSERT INTO activity_events(occurred_at,kind,object_id,title,state) SELECT unixepoch(),'related',NEW.left_id,m.title,'related title linked' FROM media_items m WHERE m.id=NEW.left_id;
END;
CREATE TRIGGER event_related_removed AFTER DELETE ON related_titles BEGIN
 INSERT INTO activity_events(occurred_at,kind,object_id,title,state) SELECT unixepoch(),'related',OLD.left_id,m.title,'related title unlinked' FROM media_items m WHERE m.id=OLD.left_id;
END;
CREATE TRIGGER event_series_rules_added AFTER INSERT ON series_release_rules BEGIN
 INSERT INTO activity_events(occurred_at,kind,object_id,title,state) SELECT unixepoch(),'series_rules',NEW.media_id,m.title,'release rules saved' FROM media_items m WHERE m.id=NEW.media_id;
END;
CREATE TRIGGER event_series_rules_changed AFTER UPDATE OF revision ON series_release_rules WHEN OLD.revision<>NEW.revision BEGIN
 INSERT INTO activity_events(occurred_at,kind,object_id,title,state) SELECT unixepoch(),'series_rules',NEW.media_id,m.title,'release rules updated' FROM media_items m WHERE m.id=NEW.media_id;
END;
CREATE TRIGGER event_profile_added AFTER INSERT ON quality_profiles BEGIN
 INSERT INTO activity_events(occurred_at,kind,object_id,title,state) VALUES(unixepoch(),'automation',NEW.id,NEW.name,'quality profile added');
END;
CREATE TRIGGER event_profile_changed AFTER UPDATE OF revision ON quality_profiles WHEN OLD.revision<>NEW.revision BEGIN
 INSERT INTO activity_events(occurred_at,kind,object_id,title,state) VALUES(unixepoch(),'automation',NEW.id,NEW.name,'quality profile updated');
END;
CREATE TRIGGER event_schedule_changed AFTER UPDATE OF revision ON search_schedule WHEN OLD.revision<>NEW.revision BEGIN
 INSERT INTO activity_events(occurred_at,kind,object_id,title,state) VALUES(unixepoch(),'automation',NEW.id,'Scheduled search','settings updated');
END;
CREATE TRIGGER event_assignment_added AFTER INSERT ON item_automation BEGIN
 INSERT INTO activity_events(occurred_at,kind,object_id,title,state) SELECT unixepoch(),'automation',NEW.media_id,m.title,'profile assigned' FROM media_items m WHERE m.id=NEW.media_id;
END;
CREATE TRIGGER event_assignment_changed AFTER UPDATE OF revision ON item_automation WHEN OLD.revision<>NEW.revision BEGIN
 INSERT INTO activity_events(occurred_at,kind,object_id,title,state) SELECT unixepoch(),'automation',NEW.media_id,m.title,'profile assignment updated' FROM media_items m WHERE m.id=NEW.media_id;
END;
CREATE TRIGGER event_library_assignment_added AFTER INSERT ON media_organization BEGIN
 INSERT INTO activity_events(occurred_at,kind,object_id,title,state) SELECT unixepoch(),'organization',NEW.media_id,m.title,'library assignment saved' FROM media_items m WHERE m.id=NEW.media_id;
END;
CREATE TRIGGER event_library_assignment_changed AFTER UPDATE OF revision ON media_organization WHEN OLD.revision<>NEW.revision BEGIN
 INSERT INTO activity_events(occurred_at,kind,object_id,title,state) SELECT unixepoch(),'organization',NEW.media_id,m.title,'library assignment updated' FROM media_items m WHERE m.id=NEW.media_id;
END;
CREATE TRIGGER event_request_account_added AFTER INSERT ON request_accounts BEGIN
 INSERT INTO activity_events(occurred_at,kind,object_id,title,state) VALUES(unixepoch(),'request_account',NEW.id,NEW.username,'request account created');
END;
CREATE TRIGGER event_request_account_enabled AFTER UPDATE OF disabled ON request_accounts WHEN OLD.disabled<>NEW.disabled BEGIN
 INSERT INTO activity_events(occurred_at,kind,object_id,title,state) VALUES(unixepoch(),'request_account',NEW.id,NEW.username,CASE WHEN NEW.disabled=1 THEN 'request account disabled' ELSE 'request account enabled' END);
END;
CREATE TRIGGER event_request_list_added AFTER INSERT ON request_lists BEGIN
 INSERT INTO activity_events(occurred_at,kind,object_id,title,state) VALUES(unixepoch(),'request_list',NEW.id,NEW.provider,'import list added');
END;
CREATE TRIGGER event_request_list_changed AFTER UPDATE OF revision ON request_lists WHEN OLD.revision<>NEW.revision BEGIN
 INSERT INTO activity_events(occurred_at,kind,object_id,title,state) VALUES(unixepoch(),'request_list',NEW.id,NEW.provider,'import list updated');
END;
CREATE TRIGGER event_request_list_removed AFTER DELETE ON request_lists BEGIN
 INSERT INTO activity_events(occurred_at,kind,object_id,title,state) VALUES(unixepoch(),'request_list',OLD.id,OLD.provider,'import list removed');
END;
CREATE TRIGGER event_connection_added AFTER INSERT ON integration_configs BEGIN
 INSERT INTO activity_events(occurred_at,kind,object_id,title,state) VALUES(unixepoch(),'connection',NEW.name,NEW.name,'connection saved');
END;
CREATE TRIGGER event_connection_changed AFTER UPDATE OF ciphertext ON integration_configs BEGIN
 INSERT INTO activity_events(occurred_at,kind,object_id,title,state) VALUES(unixepoch(),'connection',NEW.name,NEW.name,'connection updated');
END;
CREATE TRIGGER event_connection_removed AFTER DELETE ON integration_configs BEGIN
 INSERT INTO activity_events(occurred_at,kind,object_id,title,state) VALUES(unixepoch(),'connection',OLD.name,OLD.name,'connection removed');
END;
CREATE TRIGGER event_season_monitoring AFTER UPDATE OF monitored ON seasons WHEN OLD.monitored<>NEW.monitored BEGIN
 INSERT INTO activity_events(occurred_at,kind,object_id,title,state) SELECT unixepoch(),'series_rules',NEW.series_id,m.title,printf('season %d monitoring %s',NEW.number,CASE WHEN NEW.monitored=1 THEN 'enabled' ELSE 'paused' END) FROM media_items m WHERE m.id=NEW.series_id;
END;
CREATE TRIGGER event_episode_monitoring AFTER UPDATE OF monitored ON episodes WHEN OLD.monitored<>NEW.monitored BEGIN
 INSERT INTO activity_events(occurred_at,kind,object_id,title,state) SELECT unixepoch(),'series_rules',s.series_id,m.title,printf('S%02dE%02d monitoring %s',s.number,NEW.number,CASE WHEN NEW.monitored=1 THEN 'enabled' ELSE 'paused' END) FROM seasons s JOIN media_items m ON m.id=s.series_id WHERE s.id=NEW.season_id;
END;
CREATE TRIGGER event_metadata_added AFTER INSERT ON media_enrichment BEGIN
 INSERT INTO activity_events(occurred_at,kind,object_id,title,state) SELECT unixepoch(),'title',m.id,m.title,'metadata updated' FROM media_items m WHERE m.id=NEW.media_id;
END;
CREATE TRIGGER event_metadata_changed AFTER UPDATE OF updated_at ON media_enrichment WHEN OLD.updated_at<>NEW.updated_at BEGIN
 INSERT INTO activity_events(occurred_at,kind,object_id,title,state) SELECT unixepoch(),'title',m.id,m.title,'metadata updated' FROM media_items m WHERE m.id=NEW.media_id;
END;
CREATE TRIGGER event_indexer_added AFTER INSERT ON indexers BEGIN
 INSERT INTO activity_events(occurred_at,kind,object_id,title,state) VALUES(unixepoch(),'indexer',NEW.id,NEW.name,'indexer added');
END;
CREATE TRIGGER event_indexer_changed AFTER UPDATE OF revision ON indexers WHEN OLD.revision<>NEW.revision BEGIN
 INSERT INTO activity_events(occurred_at,kind,object_id,title,state) VALUES(unixepoch(),'indexer',NEW.id,NEW.name,'indexer updated');
END;
CREATE TRIGGER event_indexer_removed AFTER DELETE ON indexers BEGIN
 INSERT INTO activity_events(occurred_at,kind,object_id,title,state) VALUES(unixepoch(),'indexer',OLD.id,OLD.name,'indexer removed');
END;
CREATE TRIGGER event_general_settings_changed AFTER UPDATE OF revision ON settings WHEN OLD.revision<>NEW.revision BEGIN
 INSERT INTO activity_events(occurred_at,kind,object_id,title,state) VALUES(unixepoch(),'settings',NEW.id,'Library settings','settings updated');
END;
CREATE TRIGGER event_organization_settings_changed AFTER UPDATE OF revision ON organization_config WHEN OLD.revision<>NEW.revision BEGIN
 INSERT INTO activity_events(occurred_at,kind,object_id,title,state) VALUES(unixepoch(),'organization',NEW.id,'Library organization','settings updated');
END;
CREATE TRIGGER event_youtube_collection_added AFTER INSERT ON youtube_collections BEGIN
 INSERT INTO activity_events(occurred_at,kind,object_id,title,state) VALUES(unixepoch(),'youtube',NEW.id,NEW.title,'collection created');
END;
CREATE TRIGGER event_provider_added AFTER INSERT ON provider_secrets BEGIN
 INSERT INTO activity_events(occurred_at,kind,object_id,title,state) VALUES(unixepoch(),'connection',NEW.provider,NEW.provider,'provider connected');
END;
CREATE TRIGGER event_provider_changed AFTER UPDATE OF ciphertext ON provider_secrets BEGIN
 INSERT INTO activity_events(occurred_at,kind,object_id,title,state) VALUES(unixepoch(),'connection',NEW.provider,NEW.provider,'provider updated');
END;
CREATE TRIGGER event_provider_removed AFTER DELETE ON provider_secrets BEGIN
 INSERT INTO activity_events(occurred_at,kind,object_id,title,state) VALUES(unixepoch(),'connection',OLD.provider,OLD.provider,'provider removed');
END;
CREATE TRIGGER event_webhook_added AFTER INSERT ON request_webhooks BEGIN
 INSERT INTO activity_events(occurred_at,kind,object_id,title,state) VALUES(unixepoch(),'request_account',NEW.account_id,'Discord webhook','webhook saved');
END;
CREATE TRIGGER event_webhook_changed AFTER UPDATE OF ciphertext ON request_webhooks BEGIN
 INSERT INTO activity_events(occurred_at,kind,object_id,title,state) VALUES(unixepoch(),'request_account',NEW.account_id,'Discord webhook','webhook updated');
END;
CREATE TRIGGER event_webhook_removed AFTER DELETE ON request_webhooks BEGIN
 INSERT INTO activity_events(occurred_at,kind,object_id,title,state) VALUES(unixepoch(),'request_account',OLD.account_id,'Discord webhook','webhook removed');
END;
CREATE TRIGGER event_bot_added AFTER INSERT ON discord_bot_config BEGIN
 INSERT INTO activity_events(occurred_at,kind,object_id,title,state) VALUES(unixepoch(),'request_account',NEW.id,'Discord bot','bot configured');
END;
CREATE TRIGGER event_bot_changed AFTER UPDATE OF ciphertext ON discord_bot_config BEGIN
 INSERT INTO activity_events(occurred_at,kind,object_id,title,state) VALUES(unixepoch(),'request_account',NEW.id,'Discord bot','bot updated');
END;
CREATE TRIGGER event_bot_removed AFTER DELETE ON discord_bot_config BEGIN
 INSERT INTO activity_events(occurred_at,kind,object_id,title,state) VALUES(unixepoch(),'request_account',OLD.id,'Discord bot','bot removed');
END;
CREATE TRIGGER event_bot_notice_changed AFTER UPDATE OF state ON discord_bot_notices WHEN OLD.state<>NEW.state AND NEW.state IN ('sent','uncertain') BEGIN
 INSERT INTO activity_events(occurred_at,kind,object_id,title,state) SELECT unixepoch(),'request',m.id,m.title,CASE WHEN NEW.state='sent' THEN 'admin notice sent' ELSE 'admin notice uncertain' END FROM media_requests m WHERE m.id=NEW.request_id;
END;
CREATE TRIGGER event_emby_monitor_changed AFTER UPDATE OF revision ON emby_monitor WHEN OLD.revision<>NEW.revision BEGIN
 INSERT INTO activity_events(occurred_at,kind,object_id,title,state) VALUES(unixepoch(),'playback',NEW.id,'Emby recording',CASE WHEN NEW.enabled=1 THEN 'monitoring enabled' ELSE 'monitoring paused' END);
END;
CREATE TRIGGER event_subtitle_preferences_changed AFTER UPDATE OF revision ON subtitle_preferences WHEN OLD.revision<>NEW.revision BEGIN
 INSERT INTO activity_events(occurred_at,kind,object_id,title,state) VALUES(unixepoch(),'subtitle',NEW.id,'Subtitle preferences','settings updated');
END;
CREATE TRIGGER event_anime_search_added AFTER INSERT ON anime_search_settings BEGIN
 INSERT INTO activity_events(occurred_at,kind,object_id,title,state) SELECT unixepoch(),'series_rules',NEW.media_id,m.title,'anime search settings saved' FROM media_items m WHERE m.id=NEW.media_id;
END;
CREATE TRIGGER event_anime_search_changed AFTER UPDATE OF revision ON anime_search_settings WHEN OLD.revision<>NEW.revision BEGIN
 INSERT INTO activity_events(occurred_at,kind,object_id,title,state) SELECT unixepoch(),'series_rules',NEW.media_id,m.title,'anime search settings updated' FROM media_items m WHERE m.id=NEW.media_id;
END;
CREATE TRIGGER event_media_file_removed AFTER DELETE ON media_files BEGIN
 INSERT INTO activity_events(occurred_at,kind,object_id,title,state) SELECT unixepoch(),'title',m.id,m.title,'file registration removed' FROM media_items m WHERE m.id=COALESCE(OLD.movie_id,OLD.series_id);
END;
