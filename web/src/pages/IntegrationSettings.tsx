import { useEffect, useState, type FormEvent } from "react";
import { Badge, Button, TextField } from "@radix-ui/themes";
import { Plug } from "lucide-react";
import { api, messageFor, type IntegrationStatus } from "../api";
import { RequestState } from "../components";
import { useResource } from "../useResource";

export function IntegrationSettings({
  kind,
}: {
  kind: "torznab" | "qbittorrent";
}) {
  const resource = useResource<IntegrationStatus>(`/integrations/${kind}`);
  const [url, setURL] = useState("");
  const [username, setUsername] = useState("");
  const [secret, setSecret] = useState("");
  const [downloadPath, setDownloadPath] = useState("");
  const [pending, setPending] = useState(false);
  const [error, setError] = useState("");
  const [success, setSuccess] = useState("");
  const indexer = kind === "torznab";
  const name = indexer ? "Torznab indexer" : "qBittorrent";
  useEffect(() => {
    if (resource.data) {
      setURL(resource.data.url ?? "");
      setUsername(resource.data.username ?? "");
      setDownloadPath(resource.data.download_path ?? "");
    }
  }, [resource.data]);
  async function action(type: "save" | "test" | "remove") {
    if (pending) return;
    setPending(true);
    setError("");
    setSuccess("");
    try {
      if (type === "save") {
        const body = indexer
          ? {
              url,
              ...(secret
                ? { api_key: secret }
                : resource.data?.configured
                  ? {}
                  : { api_key: "" }),
            }
          : {
              url,
              username,
              download_path: downloadPath,
              ...(secret ? { password: secret } : {}),
            };
        await api(`/integrations/${kind}`, {
          method: "PUT",
          body: JSON.stringify(body),
        });
        setSecret("");
        resource.reload();
        setSuccess(`${name} connection verified and saved.`);
      }
      if (type === "test") {
        await api(`/integrations/${kind}/test`, { method: "POST", body: "{}" });
        setSuccess(`${name} connection verified.`);
      }
      if (type === "remove") {
        await api(`/integrations/${kind}`, { method: "DELETE" });
        setSecret("");
        resource.reload();
        setSuccess(`${name} disconnected.`);
      }
    } catch (e) {
      setError(messageFor(e));
    } finally {
      setPending(false);
    }
  }
  function submit(e: FormEvent) {
    e.preventDefault();
    void action("save");
  }
  return (
    <section
      className="panel settings-info integration-settings"
      aria-label={`${name} settings`}
    >
      <div className="section-title">
        <h2>
          <Plug size={19} />
          {name}
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
            {indexer
              ? "Connect the Torznab API from Prowlarr, Jackett, or another compatible indexer to search movie releases."
              : "Send selected releases to qBittorrent, track completion, and import verified movie files into your library."}
          </p>
          <form onSubmit={submit}>
            <label htmlFor={`${kind}-url`}>
              {indexer ? "Torznab API endpoint" : "Web UI URL"}
            </label>
            <TextField.Root
              id={`${kind}-url`}
              name={`${kind}-endpoint`}
              autoComplete="off"
              type="url"
              value={url}
              onChange={(e) => setURL(e.target.value)}
              placeholder={
                indexer
                  ? "http://localhost:9696/1/api"
                  : "http://localhost:8080"
              }
              required
              maxLength={2048}
              disabled={pending}
            />
            {!indexer && (
              <>
                <label htmlFor="qbt-username">Web UI username</label>
                <TextField.Root
                  id="qbt-username"
                  name="qbittorrent-service-username"
                  value={username}
                  onChange={(e) => setUsername(e.target.value)}
                  required
                  maxLength={200}
                  autoComplete="off"
                  disabled={pending}
                />
              </>
            )}
            <label htmlFor={`${kind}-secret`}>
              {indexer ? "API key" : "Web UI password"}
            </label>
            <TextField.Root
              id={`${kind}-secret`}
              name={`${kind}-service-secret`}
              type="password"
              value={secret}
              onChange={(e) => setSecret(e.target.value)}
              required={!indexer && !resource.data.configured}
              maxLength={2048}
              autoComplete="new-password"
              disabled={pending}
            />
            <p className="field-help">
              {resource.data.configured
                ? "Leave blank to keep the saved credential. Re-enter it when changing the service URL."
                : indexer
                  ? "Public endpoints may leave the API key blank."
                  : "Use your qBittorrent Web UI credentials."}{" "}
              Credentials are encrypted locally and never returned after saving.
            </p>
            {!indexer && (
              <>
                <label htmlFor="qbt-download-path">
                  qBittorrent downloads folder
                </label>
                <TextField.Root
                  id="qbt-download-path"
                  value={downloadPath}
                  onChange={(e) => setDownloadPath(e.target.value)}
                  placeholder="/downloads"
                  required
                  maxLength={2048}
                  disabled={pending}
                />
                <p className="field-help">
                  The folder as qBittorrent sees it. Mount that same folder at
                  ReelScarey’s Downloads path shown above. A Web UI connection
                  alone does not give ReelScarey access to files on another
                  server.
                </p>
              </>
            )}
            <Button
              type="submit"
              disabled={
                pending ||
                !url.trim() ||
                (!indexer && (!username.trim() || !downloadPath.trim()))
              }
            >
              {pending ? "Please wait…" : "Save & test connection"}
            </Button>
          </form>
          {resource.data.configured && (
            <div className="provider-actions">
              <Button
                variant="soft"
                disabled={pending}
                onClick={() => void action("test")}
              >
                Test saved connection
              </Button>
              <Button
                variant="ghost"
                color="gray"
                disabled={pending}
                onClick={() => void action("remove")}
              >
                Disconnect
              </Button>
            </div>
          )}
        </>
      )}
      {error && (
        <p role="alert" className="movie-error">
          {error}
        </p>
      )}
      {success && (
        <p role="status" className="save-success">
          {success}
        </p>
      )}
    </section>
  );
}
