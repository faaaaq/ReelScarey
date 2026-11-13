import { useEffect, useState, type FormEvent } from "react";
import { Button, TextField } from "@radix-ui/themes";
import { RotateCcw, Save } from "lucide-react";
import { Link, Navigate, useLocation, useParams } from "react-router-dom";
import {
  APIError,
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
import { LibraryRootsEditor } from "../components/LibraryRootsEditor";
import { useResource } from "../useResource";
import {
  Alert,
  DescriptionItem,
  DescriptionList,
  FieldSet,
  FormGroup,
  PageBody,
  PageToolbar,
  ToolbarButton,
  ToolbarSection,
} from "../ui";
import { TMDbSettings } from "./TMDbSettings";
import { IntegrationSettings } from "./IntegrationSettings";
import { ArrBackupImport } from "./ArrBackupImport";
import { SecuritySettings } from "./SecuritySettings";
import "./Admin.css";

type Area = { title: string; to: string; description: string };

/** Warns before leaving with unsaved form changes. */
export function useUnsavedGuard(dirty: boolean) {
  useEffect(() => {
    if (!dirty) return;
    function handler(event: BeforeUnloadEvent) {
      event.preventDefault();
      event.returnValue = "";
    }
    window.addEventListener("beforeunload", handler);
    return () => window.removeEventListener("beforeunload", handler);
  }, [dirty]);
}

const groups: { name: string; items: Area[] }[] = [
  {
    name: "Library",
    items: [
      {
        title: "Media Management",
        to: "/settings/media",
        description: "Collections, root folders and naming presets.",
      },
      {
        title: "Metadata",
        to: "/metadata-refresh",
        description: "Scheduled TMDb catalog and episode refreshes.",
      },
      {
        title: "Library Import",
        to: "/import",
        description: "Find existing files and register them in place.",
      },
      {
        title: "Duplicate Review",
        to: "/duplicates",
        description:
          "Inspect files that may be duplicates without changing anything.",
      },
      {
        title: "Music Import",
        to: "/music/import",
        description: "Scan existing music folders and register matched files.",
      },
      {
        title: "Sonarr / Radarr Import",
        to: "/settings/backup-import",
        description:
          "Add titles from a backup without importing credentials or paths.",
      },
    ],
  },
  {
    name: "Downloading",
    items: [
      {
        title: "Indexers",
        to: "/indexers",
        description: "Torrent trackers, RSS feeds, Torznab and Newznab.",
      },
      {
        title: "Announce Feeds",
        to: "/announces",
        description: "IRC announce channels for new releases.",
      },
      {
        title: "Download Clients",
        to: "/download-clients",
        description:
          "Built-in qBittorrent, external clients, defaults and VPN.",
      },
      {
        title: "Primary Connections",
        to: "/settings/clients",
        description:
          "The single Torznab, qBittorrent and SABnzbd connections used by manual searches.",
      },
      {
        title: "Automation",
        to: "/automation",
        description: "Search schedules and quality profiles.",
      },
      {
        title: "Subtitles",
        to: "/subtitles",
        description: "OpenSubtitles and SubDL, languages and downloads.",
      },
    ],
  },
  {
    name: "General",
    items: [
      {
        title: "Connect",
        to: "/settings/connect",
        description: "TMDb, requests, Discord and Emby.",
      },
      {
        title: "General",
        to: "/settings/general",
        description: "Library name, episode naming and file verification.",
      },
      {
        title: "Security",
        to: "/settings/security",
        description: "Two-factor authentication for administrator sign-in.",
      },
      {
        title: "System",
        to: "/system",
        description: "Status, health checks, backups and storage.",
      },
    ],
  },
];

const legacyHashes: Record<string, string> = {
  "#general": "general",
  "#library-name": "general",
  "#episode-naming": "general",
  "#movie-folder": "general",
  "#series-folder": "general",
  "#media": "media",
  "#connect": "connect",
  "#tmdb": "connect",
  "#indexers": "clients",
  "#torznab": "clients",
  "#clients": "clients",
  "#qbittorrent": "clients",
  "#sabnzbd": "clients",
  "#system": "general",
};

export function Settings() {
  const { section } = useParams();
  const location = useLocation();
  if (!section) {
    const legacy = legacyHashes[location.hash];
    if (legacy)
      return (
        <Navigate
          to={`/settings/${legacy}${legacy === "clients" ? location.hash : ""}`}
          replace
        />
      );
    return <SettingsIndex />;
  }
  if (section === "general") return <GeneralSettings />;
  if (section === "media") return <MediaSettings />;
  if (section === "connect") return <ConnectSettings />;
  if (section === "security") return <SecuritySettings />;
  if (section === "clients") return <ConnectionSettings />;
  if (section === "backup-import") return <ArrBackupImport />;
  return <Navigate to="/settings" replace />;
}

function SettingsIndex() {
  return (
    <PageBody>
      <PageHeading title="Settings" />
      {groups.map((group) => (
        <FieldSet key={group.name} legend={group.name}>
          <ul className="adm-settings-list">
            {group.items.map((item) => (
              <li key={item.to}>
                <Link to={item.to}>{item.title}</Link>
                <p>{item.description}</p>
              </li>
            ))}
          </ul>
        </FieldSet>
      ))}
    </PageBody>
  );
}

function pick(value: LibrarySettings): SettingsUpdate {
  const {
    library_name,
    movie_folder,
    series_folder,
    episode_naming,
    register_unverified_video,
    import_mode,
    nfo_export,
    artwork_export,
    revision,
  } = value;
  return {
    library_name,
    movie_folder,
    series_folder,
    episode_naming,
    register_unverified_video,
    import_mode: import_mode ?? "hardlink",
    nfo_export: nfo_export ?? false,
    artwork_export: artwork_export ?? false,
    revision,
  };
}

function GeneralSettings() {
  const settings = useResource<LibrarySettings>("/settings");
  const system = useResource<System>("/system");
  const [form, setForm] = useState<SettingsUpdate>();
  const [saved, setSaved] = useState<SettingsUpdate>();
  const [saving, setSaving] = useState(false);
  const [error, setError] = useState("");
  const [success, setSuccess] = useState(false);

  useEffect(() => {
    if (settings.data) {
      const value = pick(settings.data);
      setForm(value);
      setSaved(value);
      setError("");
      setSuccess(false);
    }
  }, [settings.data]);

  const dirty = JSON.stringify(form) !== JSON.stringify(saved);
  useUnsavedGuard(dirty);
  function update<K extends keyof Omit<SettingsUpdate, "revision">>(
    field: K,
    value: SettingsUpdate[K],
  ) {
    setForm((current) => (current ? { ...current, [field]: value } : current));
    setSuccess(false);
  }
  async function submit(event?: FormEvent) {
    event?.preventDefault();
    if (!form || saving || !dirty) return;
    setSaving(true);
    setError("");
    setSuccess(false);
    try {
      const value = pick(
        await api<LibrarySettings>("/settings", {
          method: "PUT",
          body: JSON.stringify(form),
        }),
      );
      setForm(value);
      setSaved(value);
      setSuccess(true);
    } catch (e) {
      if (e instanceof APIError && e.status === 409) {
        // Someone else saved settings first. Reload the saved values so the
        // form and saved snapshot rebase from the server and the conflict
        // loop ends; the current edit is discarded.
        settings.reload();
        setError(
          "Your edit was discarded because the settings changed while you were working. The saved settings have been reloaded — review them and apply your changes again.",
        );
      } else {
        setError(messageFor(e));
      }
    } finally {
      setSaving(false);
    }
  }

  return (
    <>
      <PageToolbar>
        <ToolbarSection>
          <ToolbarButton
            icon={Save}
            label={saving ? "Saving…" : "Save Changes"}
            disabled={!dirty || saving}
            onClick={() => void submit()}
          />
          <ToolbarButton
            icon={RotateCcw}
            label="Reset"
            disabled={!dirty || saving}
            onClick={() => {
              setForm(saved);
              setError("");
              setSuccess(false);
            }}
          />
        </ToolbarSection>
      </PageToolbar>
      <PageBody>
        <PageHeading title="General" />
        <RequestState
          loading={settings.loading}
          error={settings.error}
        code={settings.code}
          retry={settings.reload}
        />
        {form && (
          <form className="rs-stack" onSubmit={submit}>
            <fieldset disabled={saving} className="rs-stack">
              <FieldSet legend="Library">
                <FormGroup
                  label="Library name"
                  htmlFor="library-name"
                  help="Shown on the dashboard."
                >
                  <TextField.Root
                    id="library-name"
                    value={form.library_name}
                    onChange={(e) => update("library_name", e.target.value)}
                    maxLength={80}
                    required
                    autoComplete="off"
                  />
                </FormGroup>
                <FormGroup
                  label="TV episode names"
                  htmlFor="episode-naming"
                  help="Multi-episode files use S01E01-E02. Existing files keep their names."
                >
                  <select
                    id="episode-naming"
                    value={form.episode_naming ?? "standard"}
                    onChange={(e) =>
                      update(
                        "episode_naming",
                        e.target.value as SettingsUpdate["episode_naming"],
                      )
                    }
                  >
                    <option value="standard">Show - S01E01</option>
                    <option value="with-title">
                      Show - S01E01 - Episode title
                    </option>
                  </select>
                </FormGroup>
              </FieldSet>
              <FieldSet
                legend="Default folders"
                description={
                  <>
                    Used only when no collection rules are saved. Collections,
                    including Anime, are managed in{" "}
                    <Link to="/settings/media">Media Management</Link>. Saving
                    creates no folders.
                  </>
                }
              >
                <FormGroup label="Movie folder" htmlFor="movie-folder">
                  <TextField.Root
                    id="movie-folder"
                    value={form.movie_folder}
                    onChange={(e) => update("movie_folder", e.target.value)}
                    maxLength={80}
                    required
                  />
                </FormGroup>
                <FormGroup label="TV folder" htmlFor="series-folder">
                  <TextField.Root
                    id="series-folder"
                    value={form.series_folder}
                    onChange={(e) => update("series_folder", e.target.value)}
                    maxLength={80}
                    required
                  />
                </FormGroup>
              </FieldSet>
              <FieldSet
                legend="File imports"
                description="Controls how completed downloads enter the library. ReelScarey still uses private, no-overwrite hardlinks inside the library for publication and reorganization."
              >
                <FormGroup
                  label="Library import method"
                  htmlFor="import-mode"
                  help="Hardlink saves space only when downloads and the library share the same writable mounted filesystem. Otherwise the import automatically uses a verified copy. Always copy writes an independent library copy and needs space for both files while the download seeds. Symbolic-link imports are not supported because removing a seeding source would break the library file."
                >
                  <select
                    id="import-mode"
                    value={form.import_mode}
                    onChange={(e) =>
                      update(
                        "import_mode",
                        e.target.value as SettingsUpdate["import_mode"],
                      )
                    }
                  >
                    <option value="hardlink">
                      Hardlink when possible (default)
                    </option>
                    <option value="copy">Always copy</option>
                  </select>
                </FormGroup>
                <FormGroup
                  label="Info sidecars"
                  help="Write optional Jellyfin-compatible .nfo sidecars beside registered media. Existing files are never replaced. After a reorganization, an older sidecar may remain beside the previous path."
                >
                  <label className="rs-check">
                    <input
                      type="checkbox"
                      aria-label="Write .nfo sidecars for registered media"
                      checked={form.nfo_export}
                      onChange={(e) => update("nfo_export", e.target.checked)}
                    />
                    <span>Write .nfo sidecars for registered media</span>
                  </label>
                </FormGroup>
                <FormGroup
                  label="Poster sidecars"
                  help="Write a missing poster.jpg beside registered movies from TMDb artwork. Existing images are never replaced; TV episode artwork is not written."
                >
                  <label className="rs-check">
                    <input
                      type="checkbox"
                      aria-label="Write poster.jpg for registered movies"
                      checked={form.artwork_export}
                      onChange={(e) =>
                        update("artwork_export", e.target.checked)
                      }
                    />
                    <span>Write movie poster.jpg sidecars</span>
                  </label>
                </FormGroup>
              </FieldSet>
              <FieldSet legend="File verification">
                <FormGroup
                  label="Unverified videos"
                  help="Registered files that ffprobe cannot inspect are labeled unverified: resolution and embedded titles stay unknown, and subtitle matching skips them. They are often incomplete downloads."
                >
                  <label className="rs-check">
                    <input
                      type="checkbox"
                      aria-label="Register videos that cannot be verified"
                      checked={form.register_unverified_video}
                      onChange={(e) =>
                        update("register_unverified_video", e.target.checked)
                      }
                    />
                    <span>Register videos that ffprobe cannot verify</span>
                  </label>
                </FormGroup>
              </FieldSet>
            </fieldset>
            {error && (
              <Alert kind="danger">
                <p>{error}</p>
                <div>
                  <Button
                    type="button"
                    size="1"
                    variant="soft"
                    color="gray"
                    onClick={settings.reload}
                  >
                    Reload saved settings
                  </Button>
                </div>
              </Alert>
            )}
            {success && <Alert kind="success">Preferences saved.</Alert>}
            <button type="submit" hidden aria-hidden="true" tabIndex={-1} />
          </form>
        )}
        <FieldSet legend="Server">
          <RequestState
            loading={system.loading}
            error={system.error}
        code={system.code}
            retry={system.reload}
          />
          {system.data && (
            <>
              <p className="rs-muted">
                Paths are set with environment variables when the container
                starts.
              </p>
              <DescriptionList>
                <DescriptionItem title="Version">
                  {system.data.version}
                </DescriptionItem>
                {Object.entries(system.data.paths ?? {}).map(([key, value]) => (
                  <DescriptionItem key={key} title={key}>
                    <code>{value}</code>
                  </DescriptionItem>
                ))}
              </DescriptionList>
            </>
          )}
        </FieldSet>
      </PageBody>
    </>
  );
}

function MediaSettings() {
  const collections = useResource<CollectionConfig>("/organization");
  return (
    <PageBody>
      <PageHeading title="Media Management" />
      <RequestState
        loading={collections.loading}
        error={collections.error}
        code={collections.code}
        retry={collections.reload}
      />
      {collections.data?.libraries && (
        <CollectionSettings
          key={collections.data.revision}
          config={collections.data}
          saved={collections.reload}
        />
      )}
      <LibraryRootsEditor />
      <FieldSet legend="Related">
        <ul className="adm-settings-list">
          <li>
            <Link to="/organization">Reorganize registered files</Link>
            <p>Naming templates and reviewed renames of existing files.</p>
          </li>
          <li>
            <Link to="/import">Library Import</Link>
            <p>Match existing files and register them without moving them.</p>
          </li>
          <li>
            <Link to="/duplicates">Duplicate review</Link>
            <p>
              Inspect probable duplicates and files whose checks disagree. This
              review is read-only.
            </p>
          </li>
          <li>
            <Link to="/metadata-refresh">Metadata</Link>
            <p>Schedule catalog and episode metadata updates.</p>
          </li>
        </ul>
      </FieldSet>
    </PageBody>
  );
}

function ConnectSettings() {
  return (
    <PageBody>
      <PageHeading title="Connect" />
      <TMDbSettings />
      <FieldSet legend="Other connections">
        <ul className="adm-settings-list">
          <li>
            <Link to="/subtitles">Subtitle providers</Link>
            <p>OpenSubtitles and SubDL accounts, languages and downloads.</p>
          </li>
          <li>
            <Link to="/requests">Requests</Link>
            <p>Request accounts, lists and Discord notifications.</p>
          </li>
          <li>
            <Link to="/watch-activity">Emby</Link>
            <p>Playback reported by a connected Emby server.</p>
          </li>
        </ul>
      </FieldSet>
    </PageBody>
  );
}

function ConnectionSettings() {
  const location = useLocation();
  useEffect(() => {
    if (location.hash)
      document
        .getElementById(location.hash.slice(1))
        ?.scrollIntoView({ block: "start" });
  }, [location.hash]);
  return (
    <PageBody>
      <PageHeading
        title="Primary Connections"
        description="Used when no client is chosen as a default. Torrent downloads fall back to the built-in client when qBittorrent is not connected here. Named clients, defaults and the built-in client are managed in Download Clients."
      />
      <IntegrationSettings kind="torznab" />
      <IntegrationSettings kind="qbittorrent" />
      <IntegrationSettings kind="sabnzbd" />
      <p className="rs-muted">
        More indexers live in <Link to="/indexers">Indexers</Link>; more clients
        in <Link to="/download-clients">Download Clients</Link>.
      </p>
    </PageBody>
  );
}
