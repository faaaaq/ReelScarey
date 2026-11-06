import { useEffect, useState, type FormEvent, type ReactNode } from "react";
import { Button, TextField } from "@radix-ui/themes";
import { Link, useLocation } from "react-router-dom";
import {
  CheckCircle2,
  Database,
  Download,
  FolderOpen,
  Plug,
  Radio,
  Save,
  Server,
  Settings2,
} from "lucide-react";
import {
  api,
  messageFor,
  type Settings as LibrarySettings,
  type SettingsUpdate,
  type System,
} from "../api";
import { PageHeading, RequestState } from "../components";
import {
  CollectionSettings,
  type CollectionConfig,
} from "../components/CollectionSettings";
import { useResource } from "../useResource";
import { TMDbSettings } from "./TMDbSettings";
import { IntegrationSettings } from "./IntegrationSettings";

const sections = [
  {
    id: "general",
    name: "General",
    icon: Settings2,
    description:
      "Library identity, folder defaults, episode naming and unverified-file behavior.",
  },
  {
    id: "media",
    name: "Media Management",
    icon: FolderOpen,
    description:
      "Collection destinations, naming presets and reviewed library changes.",
  },
  {
    id: "indexers",
    name: "Indexers",
    icon: Radio,
    description:
      "Direct tracker connections, private feeds, Torznab/Newznab and IRC announcements.",
  },
  {
    id: "clients",
    name: "Download Clients",
    icon: Download,
    description:
      "Named qBittorrent, SABnzbd and NZBGet connections, defaults and folder mappings.",
  },
  {
    id: "connect",
    name: "Connect",
    icon: Plug,
    description:
      "Metadata, subtitle providers, requests and playback reporting.",
  },
  {
    id: "system",
    name: "System",
    icon: Server,
    description: "Backups, storage, health checks and server paths.",
  },
] as const;

type SectionID = (typeof sections)[number]["id"];

function sectionFromHash(hash: string): SectionID {
  if (hash === "#qbittorrent" || hash === "#sabnzbd") return "clients";
  if (hash === "#torznab") return "indexers";
  if (hash === "#tmdb") return "connect";
  if (
    [
      "#library-name",
      "#episode-naming",
      "#movie-folder",
      "#series-folder",
    ].includes(hash)
  )
    return "general";
  const match = sections.find((section) => `#${section.id}` === hash);
  return match ? match.id : "general";
}

function SectionHead({ id }: { id: SectionID }) {
  const section = sections.find((item) => item.id === id)!;
  const Icon = section.icon;
  return (
    <div className="settings-section-head">
      <div>
        <h2>
          <Icon size={18} /> {section.name}
        </h2>
        <p>{section.description}</p>
      </div>
    </div>
  );
}

function SettingsLink({
  to,
  title,
  children,
}: {
  to: string;
  title: string;
  children: ReactNode;
}) {
  return (
    <Link className="settings-link-card" to={to}>
      <h3>{title}</h3>
      <p>{children}</p>
      <span className="text-link">Open</span>
    </Link>
  );
}

export function Settings() {
  const location = useLocation();
  const settings = useResource<LibrarySettings>("/settings");
  const system = useResource<System>("/system");
  const collections = useResource<CollectionConfig>("/organization");
  const [form, setForm] = useState<SettingsUpdate>();
  const [saved, setSaved] = useState<SettingsUpdate>();
  const [saving, setSaving] = useState(false);
  const [error, setError] = useState("");
  const [success, setSuccess] = useState(false);
  const [section, setSection] = useState<SectionID>(() =>
    sectionFromHash(location.hash),
  );
  const ready =
    !settings.loading &&
    !system.loading &&
    !settings.error &&
    !system.error &&
    !!form &&
    !!system.data;

  useEffect(() => {
    setSection(sectionFromHash(location.hash));
  }, [location.hash]);

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
  function choose(id: SectionID) {
    setSection(id);
    const next =
      id === "general" ? location.pathname : `${location.pathname}#${id}`;
    window.history.replaceState(null, "", next);
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
        description="Focused sections for the library, sources, clients and server."
      />
      <RequestState
        loading={settings.loading || system.loading}
        error={settings.error || system.error}
        retry={() => {
          settings.reload();
          system.reload();
        }}
      />
      {ready && form && system.data && (
        <div className="settings-shell">
          <nav className="settings-rail" aria-label="Settings sections">
            <p className="settings-rail-group">Settings</p>
            {sections.map((item) => {
              const Icon = item.icon;
              return (
                <button
                  key={item.id}
                  type="button"
                  aria-current={section === item.id ? "page" : undefined}
                  onClick={() => choose(item.id)}
                >
                  <Icon size={16} /> {item.name}
                </button>
              );
            })}
          </nav>
          <div className="settings-content">
            <section hidden={section !== "general"}>
              <SectionHead id="general" />
              <form className="panel settings-form" onSubmit={submit}>
                <div className="ui-section-head">
                  <div>
                    <h3>Library preferences</h3>
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
                    Collection folder names, including Anime and any custom
                    collections, are managed in{" "}
                    <Link className="text-link" to="/settings#media">
                      Media Management
                    </Link>
                    . These two fields are the legacy defaults used only when no
                    collection rules are saved; saving here creates no folders.
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
                                  register_unverified_video:
                                    event.target.checked,
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
            </section>

            <section hidden={section !== "media"}>
              <SectionHead id="media" />
              <RequestState
                loading={collections.loading}
                error={collections.error}
                retry={collections.reload}
              />
              {collections.data?.libraries && (
                <CollectionSettings
                  key={collections.data.revision}
                  config={collections.data}
                  saved={collections.reload}
                />
              )}
              <div className="settings-card-grid">
                <SettingsLink
                  to="/organization"
                  title="Folders, naming & reorganization"
                >
                  Configure Movies, TV Shows, Anime and custom collections;
                  choose presets or review existing file changes.
                </SettingsLink>
                <SettingsLink to="/import" title="Find unmatched files">
                  Discover existing files, match titles and register them
                  without moving the originals.
                </SettingsLink>
                <SettingsLink to="/metadata-refresh" title="Metadata refresh">
                  Schedule catalog and episode metadata updates.
                </SettingsLink>
              </div>
            </section>

            <section hidden={section !== "indexers"}>
              <SectionHead id="indexers" />
              <div className="settings-card-grid">
                <SettingsLink to="/indexers" title="Trackers and feeds">
                  Manage IPTorrents, XSpeeds, NYAA, private RSS, Torznab and
                  Newznab connections.
                </SettingsLink>
                <SettingsLink to="/announces" title="IRC announce feeds">
                  Watch tracker announce channels and review new releases as
                  they are posted.
                </SettingsLink>
              </div>
              <IntegrationSettings kind="torznab" />
            </section>

            <section hidden={section !== "clients"}>
              <SectionHead id="clients" />
              <div className="settings-card-grid">
                <SettingsLink
                  to="/download-clients"
                  title="Named download clients"
                >
                  Manage qBittorrent, Transmission, SABnzbd and NZBGet
                  connections, defaults and folder mappings.
                </SettingsLink>
              </div>
              <IntegrationSettings kind="qbittorrent" />
              <IntegrationSettings kind="sabnzbd" />
            </section>

            <section hidden={section !== "connect"}>
              <SectionHead id="connect" />
              <TMDbSettings />
              <div className="settings-card-grid">
                <SettingsLink to="/subtitles" title="Subtitle providers">
                  Manage OpenSubtitles, languages and automatic downloads for
                  imported media.
                </SettingsLink>
                <SettingsLink to="/requests" title="Requests">
                  Manage request accounts, Discord notifications and requested
                  titles.
                </SettingsLink>
                <SettingsLink to="/watch-activity" title="Emby activity">
                  Review playback reported by the connected Emby server.
                </SettingsLink>
              </div>
            </section>

            <section hidden={section !== "system"}>
              <SectionHead id="system" />
              <div className="settings-card-grid">
                <SettingsLink to="/backups" title="Backups">
                  Create, schedule, download and restore verified backups.
                </SettingsLink>
                <SettingsLink to="/storage" title="Storage">
                  Browse mounted folders and plan storage migration.
                </SettingsLink>
                <SettingsLink to="/system" title="System checks">
                  Verify storage, database, credentials and publication.
                </SettingsLink>
                <SettingsLink to="/setup" title="Setup">
                  Walk through library, provider and client setup.
                </SettingsLink>
                <SettingsLink to="/timeline" title="Event timeline">
                  Review recorded changes across the library and jobs.
                </SettingsLink>
              </div>
              <section className="panel">
                <div className="section-title">
                  <h2>
                    <Database size={17} /> Server & storage
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
            </section>
          </div>
        </div>
      )}
    </>
  );
}
