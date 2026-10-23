import { useState, type FormEvent } from "react";
import { Badge, Button, TextField } from "@radix-ui/themes";
import { CheckCircle2, KeyRound, Plug, Unplug } from "lucide-react";
import { api, messageFor, type ProviderStatus } from "../api";
import { RequestState } from "../components";
import { useResource } from "../useResource";

export function TMDbSettings() {
  const resource = useResource<ProviderStatus>("/providers/tmdb");
  const [token, setToken] = useState("");
  const [pending, setPending] = useState(false);
  const [error, setError] = useState("");
  const [success, setSuccess] = useState("");
  async function action(kind: "save" | "test" | "remove") {
    if (pending) return;
    setPending(true);
    setError("");
    setSuccess("");
    try {
      if (kind === "save") {
        await api("/providers/tmdb", {
          method: "PUT",
          body: JSON.stringify({ token }),
        });
        setToken("");
        resource.reload();
        setSuccess("TMDb connection verified and saved.");
      }
      if (kind === "test") {
        await api("/providers/tmdb/test", { method: "POST", body: "{}" });
        setSuccess("TMDb connection verified.");
      }
      if (kind === "remove") {
        await api("/providers/tmdb", { method: "DELETE" });
        setToken("");
        resource.reload();
        setSuccess(
          "TMDb disconnected. Your saved movies and shows are still in the library.",
        );
      }
    } catch (error) {
      setError(messageFor(error));
    } finally {
      setPending(false);
    }
  }
  function submit(event: FormEvent) {
    event.preventDefault();
    void action("save");
  }
  return (
    <section
      className="panel settings-info tmdb-settings"
      aria-label="TMDb settings"
    >
      <div className="section-title">
        <h2>
          <Plug size={19} /> TMDb metadata
        </h2>
        <Badge color={resource.data?.configured ? "green" : "gray"}>
          {resource.data?.configured ? "Configured" : "Not connected"}
        </Badge>
      </div>
      <RequestState
        loading={resource.loading}
        error={resource.error}
        retry={resource.reload}
      />
      {resource.data && !resource.error && (
        <>
          <p>
            Connect The Movie Database to search for movies and TV shows, see
            their details, and add them to your library.
          </p>
          <form onSubmit={submit}>
            <label htmlFor="tmdb-token">API Read Access Token</label>
            <TextField.Root
              id="tmdb-token"
              type="password"
              value={token}
              onChange={(e) => {
                setToken(e.target.value);
                setSuccess("");
              }}
              size="3"
              maxLength={4096}
              required
              autoComplete="off"
              disabled={pending}
            >
              <TextField.Slot>
                <KeyRound size={16} />
              </TextField.Slot>
            </TextField.Root>
            <p className="field-help">
              Use the Read Access Token from your{" "}
              <a
                href="https://www.themoviedb.org/settings/api"
                target="_blank"
                rel="noreferrer"
                className="text-link"
              >
                TMDb API settings
              </a>
              . Your token is encrypted locally and never returned to the
              browser after saving.
            </p>
            <Button type="submit" disabled={pending || !token.trim()}>
              {pending
                ? "Please wait…"
                : resource.data.configured
                  ? "Replace & test token"
                  : "Save & test connection"}
            </Button>
          </form>
          {resource.data.configured && (
            <div className="provider-actions">
              <Button
                type="button"
                variant="soft"
                disabled={pending}
                onClick={() => void action("test")}
              >
                Test saved connection
              </Button>
              <Button
                type="button"
                variant="ghost"
                color="gray"
                disabled={pending}
                onClick={() => void action("remove")}
              >
                <Unplug size={15} /> Disconnect
              </Button>
            </div>
          )}
        </>
      )}
      {error && (
        <p className="form-error" role="alert">
          {error}
        </p>
      )}
      {success && (
        <p className="save-success" role="status">
          <CheckCircle2 size={17} />
          {success}
        </p>
      )}
      <div className="tmdb-credit">
        <a href="https://www.themoviedb.org" target="_blank" rel="noreferrer">
          <img
            src="/tmdb.svg"
            alt="The Movie Database"
            width="100"
            height="20"
          />
        </a>
        <p>
          This product uses the TMDB API but is not endorsed or certified by
          TMDB.
        </p>
      </div>
    </section>
  );
}
