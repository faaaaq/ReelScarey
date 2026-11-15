import { useEffect, useState, type FormEvent } from "react";
import { Button, TextField } from "@radix-ui/themes";
import { Disc3, Plus, RefreshCw } from "lucide-react";
import { Link } from "react-router-dom";
import {
  Alert,
  Check,
  ConfirmDialog,
  EmptyState,
  FieldSet,
  Label,
  Modal,
  PageBody,
  PageToolbar,
  Table,
  ToolbarButton,
  ToolbarSection,
} from "../ui";
import { PageHeading, RequestState } from "../components";
import {
  api,
  messageFor,
  APIError,
  clearCache,
  deleteCustomProfile,
  saveCustomProfile,
  updateCustomProfile,
  type MusicFallbackPolicy,
  type MusicOriginalRetention,
  type MusicPreferences,
  type MusicStreamingQuality,
  type QualityProfile,
  type StreamCacheStats,
} from "../api";
import { useResource } from "../useResource";
import {
  STREAMING_QUALITY_OPTIONS,
  profileLabel,
  profileSummary,
  useMusicProfiles,
} from "../musicProfiles";
import "./Music.css";

const CUSTOM_CODECS = ["flac", "mp3", "aac", "opus"] as const;
type CustomCodec = (typeof CUSTOM_CODECS)[number];

function clampInt(value: number, min: number, max: number) {
  if (!Number.isFinite(value)) return min;
  return Math.max(min, Math.min(max, Math.round(value)));
}

/** Create/edit form for a custom quality profile. */
function ProfileEditor({
  editing,
  onClose,
  onSaved,
}: {
  editing: QualityProfile | null;
  onClose: () => void;
  onSaved: () => void;
}) {
  const [name, setName] = useState("");
  const [codec, setCodec] = useState<CustomCodec>("opus");
  const [bitrate, setBitrate] = useState("128");
  const [vbr, setVbr] = useState(true);
  const [compression, setCompression] = useState("5");
  const [requireLossless, setRequireLossless] = useState(false);
  const [keepOriginal, setKeepOriginal] = useState(false);
  const [pending, setPending] = useState(false);
  const [error, setError] = useState("");

  useEffect(() => {
    if (!editing) return;
    setName(editing.name);
    setCodec(
      (CUSTOM_CODECS as readonly string[]).includes(editing.codec)
        ? (editing.codec as CustomCodec)
        : "opus",
    );
    setBitrate(String(editing.bitrate_kbps || 0));
    setVbr(editing.vbr);
    setCompression(String(editing.compression_level ?? 5));
    setRequireLossless(editing.require_lossless);
    setKeepOriginal(editing.keep_original);
    setError("");
  }, [editing]);

  async function save() {
    if (pending) return;
    const trimmed = name.trim();
    if (trimmed === "") {
      setError("Give the profile a name.");
      return;
    }
    const rate = clampInt(Number.parseInt(bitrate, 10), 0, 320);
    if (codec !== "flac" && (rate <= 0 || Number.isNaN(rate))) {
      setError("Lossy profiles need a bitrate between 1 and 320 kbps.");
      return;
    }
    setPending(true);
    setError("");
    const payload: QualityProfile = {
      name: trimmed,
      codec,
      bitrate_kbps: codec === "flac" ? 0 : rate,
      vbr: codec === "flac" ? false : vbr,
      compression_level:
        codec === "flac" ? clampInt(Number.parseInt(compression, 10), 0, 8) : 0,
      require_lossless: requireLossless,
      keep_original: keepOriginal,
    };
    try {
      if (editing?.id) await updateCustomProfile(editing.id, payload);
      else await saveCustomProfile(payload);
      onSaved();
      onClose();
    } catch (e) {
      setError(messageFor(e));
    } finally {
      setPending(false);
    }
  }

  return (
    <Modal
      open
      onOpenChange={(open) => {
        if (!open && !pending) onClose();
      }}
      title={editing ? "Edit custom profile" : "New custom profile"}
      description="Custom profiles are conversions you define yourself. Builtin profiles cannot be edited."
      footer={
        <>
          <Button
            variant="soft"
            color="gray"
            disabled={pending}
            onClick={onClose}
          >
            Cancel
          </Button>
          <Button disabled={pending} onClick={() => void save()}>
            {pending ? "Saving…" : "Save profile"}
          </Button>
        </>
      }
    >
      <div className="mus-search">
        <TextField.Root
          aria-label="Profile name"
          placeholder="Profile name"
          maxLength={80}
          value={name}
          onChange={(e) => setName(e.target.value)}
        />
        <label>
          Codec{" "}
          <select
            aria-label="Profile codec"
            value={codec}
            onChange={(e) => setCodec(e.target.value as CustomCodec)}
          >
            {CUSTOM_CODECS.map((value) => (
              <option key={value} value={value}>
                {value.toUpperCase()}
              </option>
            ))}
          </select>
        </label>
      </div>
      <div className="mus-search">
        <label>
          Bitrate (kbps){" "}
          <TextField.Root
            aria-label="Profile bitrate"
            inputMode="numeric"
            value={codec === "flac" ? "0" : bitrate}
            disabled={codec === "flac"}
            onChange={(e) => setBitrate(e.target.value)}
            style={{ width: 90 }}
          />
        </label>
        <label>
          Compression level (FLAC){" "}
          <select
            aria-label="Profile compression level"
            value={compression}
            disabled={codec !== "flac"}
            onChange={(e) => setCompression(e.target.value)}
          >
            {[0, 2, 4, 5, 6, 8].map((level) => (
              <option key={level} value={level}>
                {level}
              </option>
            ))}
          </select>
        </label>
        <label>
          <input
            type="checkbox"
            aria-label="Variable bitrate"
            checked={vbr}
            disabled={codec === "flac"}
            onChange={(e) => setVbr(e.target.checked)}
          />{" "}
          Variable bitrate
        </label>
      </div>
      <Check
        label="Require lossless source"
        description="Only convert files whose source is lossless."
        checked={requireLossless}
        onChange={setRequireLossless}
      />
      <Check
        label="Keep the original file"
        description="The original registered file stays in the library after conversion."
        checked={keepOriginal}
        onChange={setKeepOriginal}
      />
      {error && <Alert kind="danger">{error}</Alert>}
    </Modal>
  );
}

export function MusicSettings() {
  const prefs = useResource<MusicPreferences>("/library/music/preferences");
  const profiles = useMusicProfiles();
  const cache = useResource<StreamCacheStats>("/library/music/cache");

  const [downloadProfile, setDownloadProfile] = useState("original");
  const [streamingQuality, setStreamingQuality] =
    useState<MusicStreamingQuality>("balanced");
  const [retention, setRetention] = useState<MusicOriginalRetention>("keep");
  const [fallback, setFallback] = useState<MusicFallbackPolicy>("best");
  const [transcodingEnabled, setTranscodingEnabled] = useState(true);
  const [cacheLimit, setCacheLimit] = useState("512");
  const [maxConversions, setMaxConversions] = useState("2");
  const [pending, setPending] = useState(false);
  const [error, setError] = useState("");
  const [message, setMessage] = useState("");

  const [editorOpen, setEditorOpen] = useState(false);
  const [editing, setEditing] = useState<QualityProfile | null>(null);

  useEffect(() => {
    if (!prefs.data) return;
    setDownloadProfile(prefs.data.download_profile || "original");
    setStreamingQuality(prefs.data.streaming_quality);
    setRetention(prefs.data.original_retention);
    setFallback(prefs.data.fallback_policy);
    setTranscodingEnabled(prefs.data.transcoding_enabled);
    setCacheLimit(String(prefs.data.cache_limit_mb));
    setMaxConversions(String(prefs.data.max_conversions));
  }, [prefs.data]);

  function preferencesBody(downloadProfile: string) {
    if (!prefs.data) return null;
    return {
      allow_lossless: prefs.data.allow_lossless,
      min_bitrate: prefs.data.min_bitrate,
      preferred_formats: prefs.data.preferred_formats,
      download_profile: downloadProfile,
      streaming_quality: streamingQuality,
      original_retention: retention,
      fallback_policy: fallback,
      transcoding_enabled: transcodingEnabled,
      cache_limit_mb: clampInt(Number.parseInt(cacheLimit, 10), 64, 4096),
      max_conversions: clampInt(Number.parseInt(maxConversions, 10), 1, 4),
      revision: prefs.data.revision,
    };
  }

  async function savePreferences(e: FormEvent) {
    e.preventDefault();
    const body = preferencesBody(downloadProfile);
    if (!body || pending) return;
    setPending(true);
    setError("");
    setMessage("");
    try {
      await api("/library/music/preferences", {
        method: "PUT",
        body: JSON.stringify(body),
      });
      prefs.reload();
      setMessage("Transcoding settings saved.");
    } catch (e) {
      if (
        e instanceof APIError &&
        (e.status === 409 || e.code === "revision_conflict")
      ) {
        // Someone saved music settings first. Rebase the form from the server
        // so the conflict loop ends; the current edit is discarded.
        prefs.reload();
        setError(
          "The music settings changed while you were working. The saved settings have been reloaded — review them and apply your changes again.",
        );
      } else {
        setError(messageFor(e));
      }
    } finally {
      setPending(false);
    }
  }

  function openEditor(profile: QualityProfile | null) {
    setEditing(profile);
    setEditorOpen(true);
  }

  const profileItems = profiles.profiles;
  const selectableProfiles = profileItems.filter(
    (profile) => profile.key !== "",
  );

  return (
    <>
      <PageToolbar label="Music settings actions">
        <ToolbarSection>
          <ToolbarButton
            icon={RefreshCw}
            label="Refresh"
            spinning={prefs.loading || profiles.loading || cache.loading}
            onClick={() => {
              prefs.reload();
              profiles.reload();
              cache.reload();
            }}
          />
        </ToolbarSection>
      </PageToolbar>
      <PageBody className="mus-body">
        <PageHeading
          title="Music settings"
          description="Conversion profiles, streaming quality, retention of original files and the stream cache."
        />
        <Alert kind="info" title="How conversions run">
          Downloads, exports and library optimization queue conversion jobs that
          run in the background up to the concurrency limit below. Existing
          registered files are never replaced silently; originals are kept or
          deleted according to the retention setting.
        </Alert>
        {error && <Alert kind="danger">{error}</Alert>}
        {message && <Alert kind="success">{message}</Alert>}

        <FieldSet
          legend="Conversion settings"
          description="Applies to downloads, the player and library optimization. The general download filters stay on the Music wanted page."
        >
          <RequestState
            loading={prefs.loading}
            error={prefs.error}
            code={prefs.code}
            retry={prefs.reload}
          />
          {prefs.data && (
            <form
              className="mus-search"
              onSubmit={(e) => void savePreferences(e)}
            >
              <label>
                Default download profile{" "}
                <select
                  aria-label="Default download profile"
                  value={downloadProfile}
                  onChange={(e) => setDownloadProfile(e.target.value)}
                >
                  {selectableProfiles.map((profile) => (
                    <option key={profile.key} value={profile.key}>
                      {profileLabel(profile)}
                    </option>
                  ))}
                </select>
              </label>
              <label>
                Streaming quality{" "}
                <select
                  aria-label="Streaming quality"
                  value={streamingQuality}
                  onChange={(e) =>
                    setStreamingQuality(e.target.value as MusicStreamingQuality)
                  }
                >
                  {STREAMING_QUALITY_OPTIONS.map((option) => (
                    <option key={option.value} value={option.value}>
                      {option.label}
                    </option>
                  ))}
                </select>
              </label>
              <label>
                Original-file retention{" "}
                <select
                  aria-label="Original-file retention"
                  value={retention}
                  onChange={(e) =>
                    setRetention(e.target.value as MusicOriginalRetention)
                  }
                >
                  <option value="keep">Keep originals</option>
                  <option value="delete">Delete originals</option>
                  <option value="ask">Ask before deleting</option>
                </select>
              </label>
              <label>
                Quality fallback{" "}
                <select
                  aria-label="Quality fallback policy"
                  value={fallback}
                  onChange={(e) =>
                    setFallback(e.target.value as MusicFallbackPolicy)
                  }
                >
                  <option value="best">Use the best available source</option>
                  <option value="skip">Skip titles without the profile</option>
                  <option value="approve">Ask before falling back</option>
                </select>
              </label>
              <Check
                label="Transcoding enabled"
                description="Turn off to serve and download original files only."
                checked={transcodingEnabled}
                onChange={setTranscodingEnabled}
              />
              <label>
                Max concurrent conversions{" "}
                <select
                  aria-label="Max concurrent conversions"
                  value={maxConversions}
                  onChange={(e) => setMaxConversions(e.target.value)}
                >
                  {[1, 2, 3, 4].map((value) => (
                    <option key={value} value={value}>
                      {value}
                    </option>
                  ))}
                </select>
              </label>
              <label>
                Cache size limit (MB){" "}
                <TextField.Root
                  aria-label="Cache size limit in megabytes"
                  inputMode="numeric"
                  value={cacheLimit}
                  onChange={(e) => setCacheLimit(e.target.value)}
                  style={{ width: 90 }}
                />
              </label>
              <Button type="submit" disabled={pending}>
                Save
              </Button>
            </form>
          )}
        </FieldSet>

        <FieldSet
          legend="Quality profiles"
          description="Builtin profiles cover common targets; custom profiles add your own codec, bitrate and retention rules."
          actions={
            <Button size="1" variant="soft" onClick={() => openEditor(null)}>
              <Plus size={14} aria-hidden="true" /> New profile
            </Button>
          }
        >
          <RequestState
            loading={profiles.loading}
            error={profiles.error}
            code={profiles.code}
            retry={profiles.reload}
          />
          {profiles.data && profileItems.length === 0 && (
            <EmptyState icon={Disc3} title="No quality profiles">
              The server has not reported any builtin or custom profiles yet.
            </EmptyState>
          )}
          {profiles.data && profileItems.length > 0 && (
            <Table label="Music quality profiles">
              <thead>
                <tr>
                  <th scope="col">Name</th>
                  <th scope="col">Codec</th>
                  <th scope="col">Bitrate</th>
                  <th scope="col">Source</th>
                  <th scope="col" className="is-actions">
                    <span className="sr-only">Actions</span>
                  </th>
                </tr>
              </thead>
              <tbody>
                {profileItems.map((profile) => (
                  <tr key={profile.key || `custom-${profile.id ?? 0}`}>
                    <td>{profile.name}</td>
                    <td>{profileSummary(profile)}</td>
                    <td>
                      {profile.bitrate_kbps > 0
                        ? `${profile.bitrate_kbps} kbps`
                        : "Lossless"}
                    </td>
                    <td>
                      <Label kind={profile.custom ? "info" : "default"}>
                        {profile.custom ? "Custom" : "Builtin"}
                      </Label>
                    </td>
                    <td className="is-actions">
                      {profile.custom && (
                        <>
                          <Button
                            size="1"
                            variant="soft"
                            onClick={() => openEditor(profile)}
                          >
                            Edit
                          </Button>{" "}
                          <ConfirmDialog
                            trigger={
                              <Button size="1" variant="soft" color="red">
                                Delete
                              </Button>
                            }
                            title="Delete this custom profile?"
                            description="Removes the profile from the list. Queued conversions are not changed."
                            confirmLabel="Delete profile"
                            onConfirm={async () => {
                              if (!profile.id) return;
                              await deleteCustomProfile(profile.id);
                              profiles.reload();
                              if (downloadProfile === (profile.key ?? "")) {
                                // Persist the reset instead of only
                                // changing the local select: a deleted
                                // profile key left as the stored default
                                // would fail to resolve on the server.
                                const body = preferencesBody("original");
                                if (body) {
                                  await api("/library/music/preferences", {
                                    method: "PUT",
                                    body: JSON.stringify(body),
                                  });
                                  prefs.reload();
                                }
                                setDownloadProfile("original");
                              }
                            }}
                          />
                        </>
                      )}
                    </td>
                  </tr>
                ))}
              </tbody>
            </Table>
          )}
        </FieldSet>

        <FieldSet
          legend="Stream cache"
          description="Converted playback streams are cached on disk up to the size limit. Clearing the cache removes converted copies only; registered files are never touched."
        >
          <RequestState
            loading={cache.loading}
            error={cache.error}
            code={cache.code}
            retry={cache.reload}
          />
          {cache.data && (
            <div className="mus-scan-summary">
              <span>
                {cache.data.entries} cached{" "}
                {cache.data.entries === 1 ? "stream" : "streams"}
              </span>
              <span>Cache limit: {prefs.data?.cache_limit_mb ?? "?"} MB</span>
              <span>
                Hits {cache.data.hits} · Misses {cache.data.misses}
              </span>
              <ConfirmDialog
                trigger={
                  <Button size="1" variant="soft" color="red">
                    Clear cache
                  </Button>
                }
                title="Clear the stream cache?"
                description="Converted copies are removed from the cache folder. Registered music files are kept."
                confirmLabel="Clear cache"
                onConfirm={async () => {
                  await clearCache();
                  cache.reload();
                }}
              />
            </div>
          )}
        </FieldSet>

        <FieldSet legend="Where these settings apply" headingLevel={3}>
          <p className="rs-muted">
            Per-download profile overrides live on the download dialogs, the
            conversion queue lives on the{" "}
            <Link to="/music/organize">Organize page</Link>, and the streaming
            quality can also be changed directly in the player bar.
          </p>
        </FieldSet>
      </PageBody>
      {editorOpen && (
        <ProfileEditor
          editing={editing}
          onClose={() => setEditorOpen(false)}
          onSaved={() => profiles.reload()}
        />
      )}
    </>
  );
}
