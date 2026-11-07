import { useEffect, useState, type FormEvent } from "react";
import { Badge, Button, TextField } from "@radix-ui/themes";
import { Plug } from "lucide-react";
import { api, messageFor, type IntegrationStatus } from "../api";
import { RequestState } from "../components";
import { useResource } from "../useResource";
import "./Admin.css";

export function IntegrationSettings({
  kind,
}: {
  kind: "torznab" | "qbittorrent" | "sabnzbd";
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
  const usenet = kind === "sabnzbd";
  const name = indexer ? "Torznab indexer" : usenet ? "SABnzbd" : "qBittorrent";
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
          : usenet
            ? {
                url,
                download_path: downloadPath,
                ...(secret ? { api_key: secret } : {}),
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
      id={kind}
      className="rs-panel settings-info integration-settings"
      aria-label={`${name} settings`}
    >
      <div className="rs-panel-header">
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
              ? "Torznab endpoint from Prowlarr, Jackett or another indexer."
              : usenet
                ? "Sends NZB releases to SABnzbd."
                : "Sends torrent releases to qBittorrent."}
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
            {!indexer && !usenet && (
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
              {indexer || usenet ? "API key" : "Web UI password"}
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
            <p className="rs-form-help">
              {resource.data.configured
                ? "Leave blank to keep the saved credential. Re-enter it when changing the service URL."
                : indexer
                  ? "Public endpoints may leave the API key blank."
                  : usenet
                    ? "Use the full SABnzbd API key from Config → General, not the NZB-only key."
                    : "Use your qBittorrent Web UI credentials."}{" "}
              Credentials are encrypted locally and never returned after saving.
            </p>
            {!indexer && (
              <>
                <label htmlFor={`${kind}-download-path`}>
                  {usenet
                    ? "SABnzbd completed downloads folder"
                    : "qBittorrent downloads folder"}
                </label>
                <TextField.Root
                  id={`${kind}-download-path`}
                  value={downloadPath}
                  onChange={(e) => setDownloadPath(e.target.value)}
                  placeholder="/downloads"
                  required
                  maxLength={2048}
                  disabled={pending}
                />
                <p className="rs-form-help">
                  The folder as {name} sees it. Mount that same folder at
                  ReelScarey’s Downloads path shown above. A Web UI connection
                  alone does not give ReelScarey access to files on another
                  server.
                </p>
                {usenet && (
                  <p className="rs-form-help">
                    In SABnzbd, create category <strong>reelscarey</strong> with
                    Folder/Path <strong>ReelScarey</strong>. Disable sorting for
                    this category and choose no script. Retain its completed
                    history. ReelScarey preserves completed source files.
                  </p>
                )}
              </>
            )}
            <Button
              type="submit"
              disabled={
                pending ||
                !url.trim() ||
                (!indexer &&
                  ((!usenet && !username.trim()) || !downloadPath.trim()))
              }
            >
              {pending ? "Please wait…" : "Save & test connection"}
            </Button>
          </form>
          {resource.data.configured && (
            <div className="rs-form-actions">
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
        <p role="alert" className="rs-form-error">
          {error}
        </p>
      )}
      {success && (
        <p role="status" className="rs-alert is-success">
          {success}
        </p>
      )}
    </section>
  );
}
