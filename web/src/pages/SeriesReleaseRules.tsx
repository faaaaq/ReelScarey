import { useState, type FormEvent } from "react";
import { Button, TextField } from "@radix-ui/themes";
import { api, messageFor } from "../api";

type Rules = { codec: string; group: string; revision: number };
export function SeriesReleaseRules({ id }: { id: number }) {
  const [rules, setRules] = useState<Rules>();
  const [busy, setBusy] = useState(false);
  const [error, setError] = useState("");
  const [saved, setSaved] = useState(false);
  async function load() {
    setBusy(true);
    setError("");
    try {
      setRules(await api<Rules>(`/library/series/${id}/release-rules`));
      setSaved(false);
    } catch (e) {
      setError(messageFor(e));
    } finally {
      setBusy(false);
    }
  }
  async function save(e: FormEvent) {
    e.preventDefault();
    if (!rules) return;
    setBusy(true);
    setError("");
    try {
      setRules(
        await api<Rules>(`/library/series/${id}/release-rules`, {
          method: "PUT",
          body: JSON.stringify(rules),
        }),
      );
      setSaved(true);
    } catch (e) {
      setError(messageFor(e));
    } finally {
      setBusy(false);
    }
  }
  return (
    <section className="panel system-panel">
      <h2>Automatic release rules</h2>
      <p>
        Require a codec or release group for this show in scheduled searches, in
        addition to its quality profile. Labels must explicitly identify the
        codec and group. Manual release searches remain unrestricted.
      </p>
      {!rules ? (
        <Button variant="soft" disabled={busy} onClick={() => void load()}>
          Edit release rules
        </Button>
      ) : (
        <form onSubmit={save}>
          <label>
            Required codec{" "}
            <select
              value={rules.codec}
              onChange={(e) => {
                setSaved(false);
                setRules({ ...rules, codec: e.target.value });
              }}
            >
              <option value="">Any codec</option>
              <option value="h264">H.264 / x264 / AVC</option>
              <option value="h265">H.265 / x265 / HEVC</option>
            </select>
          </label>
          <label>
            Required release group
            <TextField.Root
              value={rules.group}
              onChange={(e) => {
                setSaved(false);
                setRules({ ...rules, group: e.target.value });
              }}
              placeholder="e.g. MeGusta"
              maxLength={80}
            />
          </label>
          <p className="field-help">
            Group must be a bracketed prefix or a hyphenated suffix in the
            release name. Existing downloads are unchanged.
          </p>
          <div className="import-actions">
            <Button disabled={busy}>Save rules</Button>
            <Button
              type="button"
              variant="soft"
              disabled={busy}
              onClick={() => void load()}
            >
              Reload
            </Button>
          </div>
          {saved && <p role="status">Release rules saved.</p>}
        </form>
      )}
      {error && (
        <p role="alert" className="error-box">
          {error}
        </p>
      )}
    </section>
  );
}
