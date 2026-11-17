import { useState } from "react";
import { Button, TextField } from "@radix-ui/themes";
import { api, messageFor } from "../api";
import { RequestState } from "../components";
import { Alert, FieldSet, FormGroup } from "../ui";
import { useResource } from "../useResource";

export function TVDBSettings() {
  const status = useResource<{ configured: boolean }>("/providers/tvdb");
  const [key, setKey] = useState("");
  const [pin, setPin] = useState("");
  const [busy, setBusy] = useState(false);
  const [error, setError] = useState("");
  const [saved, setSaved] = useState(false);

  async function save(method: "PUT" | "DELETE") {
    setBusy(true);
    setError("");
    setSaved(false);
    try {
      await api("/providers/tvdb", {
        method,
        body: method === "PUT" ? JSON.stringify({ key, pin }) : undefined,
      });
      setKey("");
      setPin("");
      setSaved(method === "PUT");
      await status.reload();
    } catch (e) {
      setError(messageFor(e));
    } finally {
      setBusy(false);
    }
  }

  return (
    <FieldSet
      legend="TheTVDB metadata"
      description="Fallback for TV searches without an exact TMDb match. TheTVDB also supplies episodes and refreshes for shows added from it."
    >
      <RequestState
        loading={status.loading}
        error={status.error}
        retry={status.reload}
      />
      {status.data && (
        <form
          className="rs-stack"
          onSubmit={(event) => {
            event.preventDefault();
            void save("PUT");
          }}
        >
          <FormGroup label="TheTVDB API key" htmlFor="tvdb-key">
            <TextField.Root
              id="tvdb-key"
              type="password"
              autoComplete="off"
              required
              maxLength={256}
              value={key}
              onChange={(event) => setKey(event.target.value)}
              disabled={busy}
            />
          </FormGroup>
          <FormGroup label="Subscriber PIN (optional)" htmlFor="tvdb-pin">
            <TextField.Root
              id="tvdb-pin"
              type="password"
              autoComplete="off"
              maxLength={128}
              value={pin}
              onChange={(event) => setPin(event.target.value)}
              disabled={busy}
            />
          </FormGroup>
          <div className="rs-form-actions">
            <Button type="submit" disabled={busy || !key.trim()}>
              {status.data.configured
                ? "Replace and test key"
                : "Save and test connection"}
            </Button>
            {status.data.configured && (
              <Button
                type="button"
                variant="soft"
                onClick={() => void save("DELETE")}
                disabled={busy}
              >
                Disconnect
              </Button>
            )}
          </div>
        </form>
      )}
      {saved && <Alert kind="success">TheTVDB connection saved.</Alert>}
      {error && (
        <Alert kind="danger" role="alert">
          {error}
        </Alert>
      )}
    </FieldSet>
  );
}
