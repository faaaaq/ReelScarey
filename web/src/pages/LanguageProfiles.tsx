import { useEffect, useState, type FormEvent } from "react";
import { Button, TextField } from "@radix-ui/themes";
import { Languages, Trash2 } from "lucide-react";
import {
  api,
  messageFor,
  type LanguageDefault,
  type LanguageProfile,
  type SubtitleTrack,
} from "../api";
import { PageHeading, RequestState } from "../components";
import {
  Alert,
  ConfirmDialog,
  EmptyState,
  FieldSet,
  FormActions,
  FormGroup,
  PageBody,
  Table,
} from "../ui";
import { useResource } from "../useResource";
import "./Admin.css";

type Draft = {
  id: number;
  name: string;
  tracks: string;
  audio: string;
  revision: number;
};

const libraryLabels: Record<LanguageDefault["library_key"], string> = {
  movies: "Movies",
  tv: "TV Shows",
  anime: "Anime",
};

function parseTracks(value: string): SubtitleTrack[] {
  return value
    .split(",")
    .map((part) => part.trim())
    .filter(Boolean)
    .map((part) => {
      const [language, variant = "full"] = part.split(":");
      return { language: language.trim(), variant: variant.trim() };
    });
}

function trackText(tracks: SubtitleTrack[]) {
  return tracks.map((track) => `${track.language}:${track.variant}`).join(", ");
}

function emptyDraft(): Draft {
  return {
    id: 0,
    name: "",
    tracks: "en:full, en:signs, en:forced",
    audio: "",
    revision: 0,
  };
}

export function LanguageProfiles() {
  const profiles = useResource<{ items: LanguageProfile[] }>(
    "/subtitles/language-profiles",
  );
  const defaults = useResource<{ items: LanguageDefault[] }>(
    "/subtitles/language-defaults",
  );
  const [draft, setDraft] = useState<Draft>();
  const [defaultItems, setDefaultItems] = useState<LanguageDefault[]>([]);
  const [busy, setBusy] = useState(false);
  const [error, setError] = useState("");
  const [notice, setNotice] = useState("");

  useEffect(() => {
    if (defaults.data) setDefaultItems(defaults.data.items);
  }, [defaults.data]);

  async function saveProfile(event: FormEvent) {
    event.preventDefault();
    if (!draft || busy) return;
    setBusy(true);
    setError("");
    setNotice("");
    try {
      const body = {
        name: draft.name.trim(),
        tracks: parseTracks(draft.tracks),
        audio_languages: draft.audio
          .split(",")
          .map((value) => value.trim())
          .filter(Boolean),
        revision: draft.revision,
      };
      if (draft.id) {
        await api(`/subtitles/language-profiles/${draft.id}`, {
          method: "PUT",
          body: JSON.stringify(body),
        });
      } else {
        await api("/subtitles/language-profiles", {
          method: "POST",
          body: JSON.stringify(body),
        });
      }
      setDraft(undefined);
      setNotice("Language profile saved.");
      profiles.reload();
    } catch (e) {
      setError(messageFor(e));
    } finally {
      setBusy(false);
    }
  }

  async function removeProfile(profile: LanguageProfile) {
    if (busy) return;
    setBusy(true);
    setError("");
    try {
      await api(`/subtitles/language-profiles/${profile.id}`, {
        method: "DELETE",
        body: "{}",
      });
      setNotice(
        `Removed “${profile.name}”. Assigned titles fall back to the library or global preferences.`,
      );
      if (draft?.id === profile.id) setDraft(undefined);
      profiles.reload();
      defaults.reload();
    } catch (e) {
      setError(messageFor(e));
      // Let the confirm dialog keep itself open and show the failure.
      throw e;
    } finally {
      setBusy(false);
    }
  }

  async function saveDefaults(event: FormEvent) {
    event.preventDefault();
    if (busy) return;
    setBusy(true);
    setError("");
    setNotice("");
    try {
      await api("/subtitles/language-defaults", {
        method: "PUT",
        body: JSON.stringify({ items: defaultItems }),
      });
      setNotice("Library language defaults saved.");
      defaults.reload();
    } catch (e) {
      setError(messageFor(e));
    } finally {
      setBusy(false);
    }
  }

  const items = profiles.data?.items ?? [];
  return (
    <PageBody>
      <PageHeading
        title="Language profiles"
        description="Ordered subtitle tracks for new and existing media. A library default overrides the global subtitle preferences; an unassigned library uses the global preferences."
      />
      <RequestState
        loading={profiles.loading || defaults.loading}
        error={profiles.error || defaults.error}
        retry={() => {
          profiles.reload();
          defaults.reload();
        }}
      />
      {error && <Alert kind="danger">{error}</Alert>}
      {notice && <Alert kind="success">{notice}</Alert>}
      <FieldSet
        legend={draft?.id ? "Edit profile" : "Add a language profile"}
        description="Use language codes such as en, es, fr, ja. Tracks are full, signs, or forced."
        actions={
          !draft ? (
            <Button type="button" onClick={() => setDraft(emptyDraft())}>
              <Languages size={16} aria-hidden="true" /> Add profile
            </Button>
          ) : undefined
        }
      >
        {draft && (
          <form onSubmit={(event) => void saveProfile(event)}>
            <FormGroup label="Name" htmlFor="language-profile-name">
              <TextField.Root
                id="language-profile-name"
                value={draft.name}
                maxLength={60}
                placeholder="English + Spanish"
                onChange={(event) =>
                  setDraft({ ...draft, name: event.target.value })
                }
              />
            </FormGroup>
            <FormGroup
              label="Subtitle tracks"
              htmlFor="language-profile-tracks"
              help="Comma-separated language:variant pairs, in preference order."
            >
              <TextField.Root
                id="language-profile-tracks"
                value={draft.tracks}
                onChange={(event) =>
                  setDraft({ ...draft, tracks: event.target.value })
                }
              />
            </FormGroup>
            <FormGroup
              label="Audio languages (optional)"
              htmlFor="language-profile-audio"
              help="Stored for display; release selection still uses the quality profile language."
            >
              <TextField.Root
                id="language-profile-audio"
                value={draft.audio}
                placeholder="en, ja"
                onChange={(event) =>
                  setDraft({ ...draft, audio: event.target.value })
                }
              />
            </FormGroup>
            <FormActions>
              <Button type="submit" disabled={busy || !draft.name.trim()}>
                Save profile
              </Button>
              <Button
                type="button"
                variant="soft"
                onClick={() => setDraft(undefined)}
              >
                Cancel
              </Button>
            </FormActions>
          </form>
        )}
      </FieldSet>
      {items.length === 0 ? (
        <EmptyState icon={Languages} title="No language profiles">
          Add a profile to request ordered subtitle tracks for a library or a
          single title. The global subtitle preferences remain the fallback.
        </EmptyState>
      ) : (
        <Table label="Language profiles">
          <thead>
            <tr>
              <th>Name</th>
              <th>Subtitle tracks</th>
              <th>Audio</th>
              <th aria-label="Actions" />
            </tr>
          </thead>
          <tbody>
            {items.map((profile) => (
              <tr key={profile.id}>
                <td>{profile.name}</td>
                <td>{trackText(profile.tracks)}</td>
                <td>{profile.audio_languages.join(", ") || "—"}</td>
                <td>
                  <div className="rs-actions">
                    <Button
                      type="button"
                      size="1"
                      variant="soft"
                      onClick={() =>
                        setDraft({
                          id: profile.id,
                          name: profile.name,
                          tracks: trackText(profile.tracks),
                          audio: profile.audio_languages.join(", "),
                          revision: profile.revision,
                        })
                      }
                    >
                      Edit
                    </Button>
                    <ConfirmDialog
                      trigger={
                        <Button
                          type="button"
                          size="1"
                          color="red"
                          variant="soft"
                          disabled={busy}
                        >
                          <Trash2 size={14} aria-hidden="true" /> Remove
                        </Button>
                      }
                      title={`Remove "${profile.name}"?`}
                      description="Assigned titles fall back to the library or global subtitle preferences. The profile is removed immediately once confirmed."
                      confirmLabel="Remove profile"
                      onConfirm={() => removeProfile(profile)}
                    />
                  </div>
                </td>
              </tr>
            ))}
          </tbody>
        </Table>
      )}
      <FieldSet
        legend="Library defaults"
        description="These defaults are used by titles without an explicit language profile."
      >
        <form onSubmit={(event) => void saveDefaults(event)}>
          <div className="automation-form-grid">
            {defaultItems.map((item) => (
              <label key={item.library_key}>
                {libraryLabels[item.library_key]}
                <select
                  value={item.profile_id ?? ""}
                  onChange={(event) =>
                    setDefaultItems((current) =>
                      current.map((candidate) =>
                        candidate.library_key === item.library_key
                          ? {
                              ...candidate,
                              profile_id: event.target.value
                                ? Number(event.target.value)
                                : null,
                            }
                          : candidate,
                      ),
                    )
                  }
                >
                  <option value="">Use global subtitle preferences</option>
                  {items.map((profile) => (
                    <option key={profile.id} value={profile.id}>
                      {profile.name}
                    </option>
                  ))}
                </select>
              </label>
            ))}
          </div>
          <FormActions>
            <Button type="submit" disabled={busy || defaultItems.length !== 3}>
              Save library defaults
            </Button>
          </FormActions>
        </form>
      </FieldSet>
    </PageBody>
  );
}
