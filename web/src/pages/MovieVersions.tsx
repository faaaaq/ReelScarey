import { useState } from "react";
import { Badge, Button } from "@radix-ui/themes";
import { Link } from "react-router-dom";
import { api, messageFor } from "../api";
import { RequestState } from "../components";
import { useResource } from "../useResource";

export type MovieVersionSettings = {
  enabled: boolean;
  hd_profile_id: number;
  uhd_profile_id: number;
  revision: number;
  hd_files: number;
  uhd_files: number;
  hd_download: string;
  uhd_download: string;
};
type Profile = { id: number; name: string; cutoff: number };

export function MovieVersions({ id }: { id: number }) {
  const settings = useResource<MovieVersionSettings>(
    `/library/movies/${id}/versions`,
  );
  const profiles = useResource<{ items: Profile[] }>("/quality-profiles");
  return (
    <section className="panel automation-panel">
      <h2>Movie versions</h2>
      <p>
        Keep all existing copies, including different resolutions and editions.
        Each file has its own subtitles and replacement history.
      </p>
      <RequestState
        loading={settings.loading || profiles.loading}
        error={settings.error || profiles.error}
        retry={() => {
          settings.reload();
          profiles.reload();
        }}
      />
      {settings.data && profiles.data && (
        <VersionForm
          key={settings.data.revision}
          id={id}
          saved={settings.data}
          profiles={profiles.data.items}
          reload={settings.reload}
        />
      )}
      <p>
        <Link
          className="text-link"
          to={`/releases?movie=${id}&movie_version=additional`}
        >
          Download another version
        </Link>
      </p>
    </section>
  );
}

function VersionForm({
  id,
  saved,
  profiles,
  reload,
}: {
  id: number;
  saved: MovieVersionSettings;
  profiles: Profile[];
  reload: () => void;
}) {
  const [enabled, setEnabled] = useState(saved.enabled);
  const [hd, setHD] = useState(saved.hd_profile_id);
  const [uhd, setUHD] = useState(saved.uhd_profile_id);
  const [busy, setBusy] = useState(false);
  const [error, setError] = useState("");
  async function save() {
    if (busy) return;
    setBusy(true);
    setError("");
    try {
      await api(`/library/movies/${id}/versions`, {
        method: "PUT",
        body: JSON.stringify({
          enabled,
          hd_profile_id: hd,
          uhd_profile_id: uhd,
          revision: saved.revision,
        }),
      });
      reload();
    } catch (e) {
      setError(messageFor(e));
    } finally {
      setBusy(false);
    }
  }
  return (
    <>
      <label className="check-label">
        <input
          type="checkbox"
          checked={enabled}
          disabled={busy}
          onChange={(e) => setEnabled(e.target.checked)}
        />
        Keep HD and 4K copies
      </label>
      <p className="field-help">
        When enabled, monitored movies can download one HD copy and one 4K copy
        using your automation schedule. HD accepts up to 1080p; 4K requires
        2160p. Turning this off keeps every file and any downloads already
        queued.
      </p>
      {enabled && (
        <div className="automation-form-grid">
          {(
            [
              {
                key: "hd",
                title: "HD",
                selected: hd,
                change: setHD,
                files: saved.hd_files,
                download: saved.hd_download,
              },
              {
                key: "uhd",
                title: "4K",
                selected: uhd,
                change: setUHD,
                files: saved.uhd_files,
                download: saved.uhd_download,
              },
            ] as const
          ).map((v) => (
            <div key={v.key}>
              <label>
                {v.title} quality profile{" "}
                <select
                  value={v.selected}
                  disabled={busy}
                  onChange={(e) => v.change(Number(e.target.value))}
                >
                  {profiles
                    .filter((p) =>
                      v.key === "hd" ? p.cutoff <= 1080 : p.cutoff === 2160,
                    )
                    .map((p) => (
                      <option value={p.id} key={p.id}>
                        {p.name}
                      </option>
                    ))}
                </select>
              </label>
              <p>
                <Badge color={v.files ? "green" : "gray"}>
                  {v.files
                    ? `${v.files} on disk`
                    : v.download
                      ? v.download.replaceAll("_", " ")
                      : "Missing"}
                </Badge>
              </p>
              {saved.enabled && (
                <Link
                  className="text-link"
                  to={`/releases?movie=${id}&movie_version=${v.key}`}
                >
                  Find {v.title} releases
                </Link>
              )}
            </div>
          ))}
        </div>
      )}
      {error && <p role="alert">{error}</p>}
      <Button
        disabled={
          busy ||
          (enabled === saved.enabled &&
            hd === saved.hd_profile_id &&
            uhd === saved.uhd_profile_id)
        }
        onClick={() => void save()}
      >
        {busy ? "Saving…" : "Save movie versions"}
      </Button>
    </>
  );
}
