import { useEffect, useState, type FormEvent } from "react";
import { Button, TextField } from "@radix-ui/themes";
import { Link, useLocation } from "react-router-dom";
import { CheckCircle2, FolderOpen, Save, Server } from "lucide-react";
import {
  api,
  messageFor,
  type Settings as LibrarySettings,
  type SettingsUpdate,
  type System,
} from "../api";
import { PageHeading, RequestState } from "../components";
import { useResource } from "../useResource";
import { TMDbSettings } from "./TMDbSettings";
import { IntegrationSettings } from "./IntegrationSettings";

const adminLinks = [
  {
    to: "/requests",
    name: "Requests",
    description: "Manage request accounts and review requested titles.",
  },
  {
    to: "/import",
    name: "Find unmatched files",
    description: "Discover and review existing files in configured folders.",
  },
  {
    to: "/organization",
    name: "Organize library",
    description: "Review collection paths, naming and file moves.",
  },
  {
    to: "/metadata-refresh",
    name: "Metadata refresh",
    description: "Schedule catalog and episode metadata updates.",
  },
  {
    to: "/storage",
    name: "Storage",
    description: "Browse mounted folders and plan storage migration.",
  },
  {
    to: "/backups",
    name: "Backups",
    description: "Create, schedule, download and restore verified backups.",
  },
  {
    to: "/timeline",
    name: "Event timeline",
    description: "Review recorded changes across the library and jobs.",
  },
  {
    to: "/watch-activity",
    name: "Emby activity",
    description: "Review playback reported by the connected Emby server.",
  },
  {
    to: "/setup",
    name: "Setup",
    description: "Walk through library, provider and client setup.",
  },
  {
    to: "/system",
    name: "System checks",
    description: "Verify storage, database, credentials and publication.",
  },
];

export function Settings() {
  const location = useLocation();
  const settings = useResource<LibrarySettings>("/settings");
  const system = useResource<System>("/system");
  const [form, setForm] = useState<SettingsUpdate>();
  const [saved, setSaved] = useState<SettingsUpdate>();
  const [saving, setSaving] = useState(false);
  const [error, setError] = useState("");
  const [success, setSuccess] = useState(false);
  const ready =
    !settings.loading &&
    !system.loading &&
    !settings.error &&
    !system.error &&
    !!form &&
    !!system.data;
  useEffect(() => {
    if (
      ready &&
      ["#tmdb", "#torznab", "#qbittorrent", "#sabnzbd"].includes(location.hash)
    ) {
      document
        .getElementById(location.hash.slice(1))
        ?.scrollIntoView({ block: "start" });
    }
  }, [ready, location.hash]);

  useEffect(() => {
    if (settings.data) {
      const {
        library_name,
        movie_folder,
        series_folder,
        episode_naming,
        register_unverified_video,
        revision,
      } = settings.data;
      const value = {
        library_name,
        movie_folder,
        series_folder,
        episode_naming,
        register_unverified_video,
        revision,
      };
      setForm(value);
      setSaved(value);
      setError("");
      setSuccess(false);
    }
  }, [settings.data]);

  const dirty = JSON.stringify(form) !== JSON.stringify(saved);
  function update(
    field: keyof Omit<SettingsUpdate, "revision">,
    value: string,
  ) {
    setForm((current) => (current ? { ...current, [field]: value } : current));
    setSuccess(false);
  }
  async function submit(event: FormEvent) {
    event.preventDefault();
    if (!form || saving) return;
    setSaving(true);
    setError("");
    setSuccess(false);
    try {
      const result = await api<LibrarySettings>("/settings", {
        method: "PUT",
        body: JSON.stringify(form),
      });
      const value = {
        library_name: result.library_name,
        movie_folder: result.movie_folder,
        series_folder: result.series_folder,
        episode_naming: result.episode_naming,
        register_unverified_video: result.register_unverified_video,
        revision: result.revision,
      };
      setForm(value);
      setSaved(value);
      setSuccess(true);
    } catch (error) {
      setError(messageFor(error));
    } finally {
      setSaving(false);
    }
  }

  return (
    <>
      <PageHeading
        eyebrow="Configuration"
        title="Settings"
        description="Library folders, naming and service connections."
      />
      <RequestState
        loading={settings.loading || system.loading}
        error={settings.error || system.error}
        retry={() => {
          settings.reload();
          system.reload();
        }}
      />
      {!settings.loading &&
        !system.loading &&
        !settings.error &&
        !system.error &&
        form &&
        system.data && (
          <div className="settings-layout">
            <form className="panel settings-form" onSubmit={submit}>
              <div className="ui-section-head">
                <div>
                  <h2>
                    <FolderOpen size={17} /> Library preferences
                  </h2>
                  <p>These preferences are saved to your local database.</p>
                </div>
                <span className="ui-chip green">Available</span>
              </div>
              <fieldset className="fieldset" disabled={saving}>
                <div>
                  <label htmlFor="library-name">Library name</label>
                  <TextField.Root
                    id="library-name"
                    value={form.library_name}
                    onChange={(event) =>
                      update("library_name", event.target.value)
                    }
                    maxLength={80}
                    required
                    size="2"
                    autoComplete="off"
                  />
                  <p className="field-help">Shown on your dashboard.</p>
                </div>
                <div>
                  <label htmlFor="episode-naming">TV episode names</label>
                  <select
                    id="episode-naming"
                    value={form.episode_naming ?? "standard"}
                    onChange={(e) => update("episode_naming", e.target.value)}
                  >
                    <option value="standard">Show - S01E01</option>
                    <option value="with-title">
                      Show - S01E01 - Episode title
                    </option>
                  </select>
                  <p className="field-help">
                    Multi-episode files use S01E01-E02. Existing files keep
                    their names.
                  </p>
                </div>
                <div>
                  <label htmlFor="movie-folder">Movie folder</label>
                  <TextField.Root
                    id="movie-folder"
                    value={form.movie_folder}
                    onChange={(event) =>
                      update("movie_folder", event.target.value)
                    }
                    maxLength={80}
                    required
                    size="2"
                    aria-describedby="folder-help"
                  />
                </div>
                <div>
                  <label htmlFor="series-folder">TV folder</label>
                  <TextField.Root
                    id="series-folder"
                    value={form.series_folder}
                    onChange={(event) =>
                      update("series_folder", event.target.value)
                    }
                    maxLength={80}
                    required
                    size="2"
                    aria-describedby="folder-help"
                  />
                </div>
                <p className="field-help span-2" id="folder-help">
                  Default folder names before organization rules are saved.
                  Organize library controls configured collection destinations.
                  Saving preferences alone creates no folders. The Current
                  ReelScarey naming preset uses this episode-name choice; other
                  presets and custom paths are in Organize library. Existing
                  files and queued import plans keep their names.
                </p>
                <div className="span-2">
                  <label className="scan-toggle">
                    <input
                      type="checkbox"
                      aria-label="Register videos that cannot be verified"
                      checked={form.register_unverified_video}
                      disabled={saving}
                      onChange={(event) => {
                        setForm((current) =>
                          current
                            ? {
                                ...current,
                                register_unverified_video: event.target.checked,
                              }
                            : current,
                        );
                        setSuccess(false);
                      }}
                    />
                    <span>
                      <strong>
                        Register videos that ffprobe cannot verify
                      </strong>
                      <small>
                        Discovery and matching add files even when the video
                        cannot be inspected, labeled as unverified: resolution
                        and embedded-title facts stay unknown, and
                        embedded-title rewrites and subtitle matching refuse
                        them. These files are often incomplete downloads —
                        replacing them is usually better.
                      </small>
                    </span>
                  </label>
                </div>
              </fieldset>
              {error && (
                <div className="form-error" role="alert">
                  <p>{error}</p>
                  <Button
                    type="button"
                    variant="soft"
                    color="gray"
                    onClick={settings.reload}
                  >
                    Reload saved settings
                  </Button>
                </div>
              )}
              <div className="form-footer">
                <Button type="submit" size="2" disabled={!dirty || saving}>
                  <Save size={16} />
                  {saving ? "Saving…" : "Save preferences"}
                </Button>
                <Button
                  type="button"
                  variant="ghost"
                  color="gray"
                  disabled={!dirty || saving}
                  onClick={() => {
                    setForm(saved);
                    setError("");
                    setSuccess(false);
                  }}
                >
                  Reset changes
                </Button>
                {success && (
                  <p className="save-success" role="status">
                    <CheckCircle2 size={16} /> Preferences saved.
                  </p>
                )}
              </div>
            </form>

            <section className="panel settings-admin">
              <div className="ui-section-head">
                <div>
                  <h2>Administration</h2>
                  <p>
                    Library tools, integrations and server maintenance live here
                    so the main navigation stays focused.
                  </p>
                </div>
              </div>
              <div className="settings-admin-grid">
                {adminLinks.map((link) => (
                  <Link
                    className="settings-link-card"
                    key={link.to}
                    to={link.to}
                  >
                    <h3>{link.name}</h3>
                    <p>{link.description}</p>
                    <span className="text-link">Open</span>
                  </Link>
                ))}
              </div>
            </section>

            <aside className="settings-side">
              <section className="panel">
                <div className="section-title">
                  <h2>
                    <Server size={17} /> Server & storage
                  </h2>
                </div>
                <p className="field-help">
                  Set these paths with environment variables when starting
                  ReelScarey.
                </p>
                <dl>
                  {Object.entries(system.data.paths).map(([key, value]) => (
                    <div key={key}>
                      <dt>{key}</dt>
                      <dd>{value}</dd>
                    </div>
                  ))}
                </dl>
                <div className="server-version">
                  <span>Version {system.data.version}</span>
                  <span className="ui-chip green">Database connected</span>
                </div>
              </section>

              <section className="settings-link-card">
                <h2>Media organization</h2>
                <p>
                  Configure Movies, TV Shows, Anime and custom collections;
                  choose naming presets or review existing file changes.
                </p>
                <Link className="text-link" to="/organization">
                  Open folders, naming & reorganization
                </Link>
              </section>

              <TMDbSettings />

              <section className="settings-link-card">
                <h2>Indexers</h2>
                <p>
                  Manage direct IPTorrents, XSpeeds, NYAA, private feeds and
                  multiple Torznab and Newznab connections.
                </p>
                <Link className="text-link" to="/indexers">
                  Manage indexers
                </Link>
              </section>
              <IntegrationSettings kind="torznab" />
              <IntegrationSettings kind="qbittorrent" />

              <section className="settings-link-card">
                <h2>Download clients</h2>
                <p>
                  Manage named qBittorrent, SABnzbd and NZBGet connections,
                  separate folder mappings and defaults for future jobs.
                </p>
                <Link className="text-link" to="/download-clients">
                  Manage download clients
                </Link>
              </section>
              <IntegrationSettings kind="sabnzbd" />

              <section className="settings-link-card">
                <h2>Subtitle providers</h2>
                <p>
                  Manage OpenSubtitles, language preferences and automatic
                  downloads for imported movies and episodes.
                </p>
                <Link className="text-link" to="/subtitles">
                  Open subtitle settings
                </Link>
              </section>
            </aside>
          </div>
        )}
    </>
  );
}
