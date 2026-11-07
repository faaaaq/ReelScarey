import { useId, useState } from "react";
import { Button } from "@radix-ui/themes";
import { Link } from "react-router-dom";
import { api, messageFor } from "../api";
import { RequestState } from "../components";
import { Alert, Check, FieldSet, FormActions, FormGroup, Label } from "../ui";
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
    <FieldSet
      legend="Versions"
      actions={
        <Button asChild size="1" variant="soft">
          <Link to={`/releases?movie=${id}&movie_version=additional`}>
            Download another version
          </Link>
        </Button>
      }
    >
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
    </FieldSet>
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
  const hdID = useId();
  const uhdID = useId();
  const dirty =
    enabled !== saved.enabled ||
    hd !== saved.hd_profile_id ||
    uhd !== saved.uhd_profile_id;
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
  const versions = [
    {
      key: "hd",
      title: "HD",
      control: hdID,
      selected: hd,
      change: setHD,
      files: saved.hd_files,
      download: saved.hd_download,
    },
    {
      key: "uhd",
      title: "4K",
      control: uhdID,
      selected: uhd,
      change: setUHD,
      files: saved.uhd_files,
      download: saved.uhd_download,
    },
  ] as const;
  return (
    <>
      <FormGroup
        label="HD and 4K"
        help="Monitored movies may get one HD copy (up to 1080p) and one 4K copy (2160p) on the automation schedule. Turning this off keeps every file and queued download."
      >
        <Check
          label="Keep HD and 4K copies"
          checked={enabled}
          disabled={busy}
          onChange={setEnabled}
        />
      </FormGroup>
      {enabled &&
        versions.map((v) => (
          <FormGroup
            key={v.key}
            label={`${v.title} quality profile`}
            htmlFor={v.control}
            help={
              <span className="rs-row">
                <Label
                  kind={v.files ? "success" : v.download ? "info" : "disabled"}
                >
                  {v.files
                    ? `${v.files} on disk`
                    : v.download
                      ? v.download.replaceAll("_", " ")
                      : "Missing"}
                </Label>
                {saved.enabled && (
                  <Link to={`/releases?movie=${id}&movie_version=${v.key}`}>
                    Find {v.title} releases
                  </Link>
                )}
              </span>
            }
          >
            <select
              id={v.control}
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
          </FormGroup>
        ))}
      {error && <Alert kind="danger">{error}</Alert>}
      <FormActions>
        <Button disabled={busy || !dirty} onClick={() => void save()}>
          {busy ? "Saving…" : "Save movie versions"}
        </Button>
      </FormActions>
    </>
  );
}
