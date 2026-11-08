import { useState, type FormEvent } from "react";
import { Button, TextField } from "@radix-ui/themes";
import { Alert, FormGroup, Label } from "../ui";
import { KeyRound, Unplug } from "lucide-react";
import { api, messageFor, type ProviderStatus } from "../api";
import { RequestState } from "../components";
import { useResource } from "../useResource";
import "./Admin.css";

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
          resource.data?.default_available
            ? "Saved token removed. Using the shared default TMDb token."
            : "TMDb disconnected. Your saved movies and shows are still in the library.",
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
    <section id="tmdb" className="rs-fieldset" aria-label="TMDb settings">
      <div className="rs-legend">
        <h2>TMDb metadata</h2>
        <Label
          kind={
            resource.data?.configured
              ? resource.data?.using_default
                ? "info"
                : "success"
              : "disabled"
          }
        >
          {resource.data?.configured
            ? resource.data.using_default
              ? "Shared default"
              : "Configured"
            : "Not connected"}
        </Label>
      </div>
      <RequestState
        loading={resource.loading}
        error={resource.error}
        retry={resource.reload}
      />
      {resource.data && !resource.error && (
        <form onSubmit={submit} className="rs-stack">
          <p className="rs-fieldset-description">
            Metadata source for movie and TV searches.
          </p>
          <FormGroup
            label="API Read Access Token"
            htmlFor="tmdb-token"
            help={
              resource.data?.using_default ? (
                <>
                  This instance uses a shared TMDb Read Access Token. Enter your
                  own token to replace it; saved movies and shows are unchanged.
                </>
              ) : (
                <>
                  The Read Access Token from your{" "}
                  <a
                    href="https://www.themoviedb.org/settings/api"
                    target="_blank"
                    rel="noreferrer"
                  >
                    TMDb API settings
                  </a>
                  . Stored encrypted; never shown again after saving.
                </>
              )
            }
          >
            <TextField.Root
              id="tmdb-token"
              type="password"
              value={token}
              onChange={(e) => {
                setToken(e.target.value);
                setSuccess("");
              }}
              maxLength={4096}
              required
              autoComplete="off"
              disabled={pending}
            >
              <TextField.Slot>
                <KeyRound size={16} />
              </TextField.Slot>
            </TextField.Root>
          </FormGroup>
          <div className="rs-form-actions adm-form-indent">
            <Button type="submit" disabled={pending || !token.trim()}>
              {pending
                ? "Please wait…"
                : resource.data.using_default
                  ? "Save my own token"
                  : resource.data.configured
                    ? "Replace & test token"
                    : "Save & test connection"}
            </Button>
            {resource.data.configured && !resource.data.using_default && (
              <>
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
                  variant="soft"
                  color={resource.data.default_available ? undefined : "red"}
                  disabled={pending}
                  onClick={() => void action("remove")}
                >
                  <Unplug size={15} />{" "}
                  {resource.data.default_available
                    ? "Use shared default"
                    : "Disconnect"}
                </Button>
              </>
            )}
          </div>
        </form>
      )}
      {error && <Alert kind="danger">{error}</Alert>}
      {success && <Alert kind="success">{success}</Alert>}
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
