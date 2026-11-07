import { useEffect, useState, type FormEvent } from "react";
import { Button, TextField } from "@radix-ui/themes";
import { Alert, FormGroup, Label } from "../ui";
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
    <section id={kind} className="rs-fieldset" aria-label={`${name} settings`}>
      <div className="rs-legend">
        <h2>{name}</h2>
        <Label kind={resource.data?.configured ? "success" : "disabled"}>
          {resource.data?.configured ? "Configured" : "Not connected"}
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
            {indexer
              ? "Torznab endpoint from Prowlarr, Jackett or another indexer."
              : usenet
                ? "Sends NZB releases to SABnzbd."
                : "Sends torrent releases to qBittorrent."}
          </p>
          <FormGroup
            label={indexer ? "Torznab API endpoint" : "Web UI URL"}
            htmlFor={`${kind}-url`}
          >
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
          </FormGroup>
          {!indexer && !usenet && (
            <FormGroup label="Web UI username" htmlFor="qbt-username">
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
            </FormGroup>
          )}
          <FormGroup
            label={indexer || usenet ? "API key" : "Web UI password"}
            htmlFor={`${kind}-secret`}
            help={
              <>
                {resource.data.configured
                  ? "Leave blank to keep the saved credential. Re-enter it when changing the URL."
                  : indexer
                    ? "Public endpoints may leave the API key blank."
                    : usenet
                      ? "Use the full SABnzbd API key from Config → General, not the NZB-only key."
                      : "Your qBittorrent Web UI credentials."}{" "}
                Stored encrypted; never shown again.
              </>
            }
          >
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
          </FormGroup>
          {!indexer && (
            <FormGroup
              label={
                usenet
                  ? "SABnzbd completed downloads folder"
                  : "qBittorrent downloads folder"
              }
              htmlFor={`${kind}-download-path`}
              help={
                <>
                  The folder as {name} sees it. Mount the same folder at
                  ReelScarey’s Downloads path.
                  {usenet && (
                    <>
                      {" "}
                      In SABnzbd, create category <strong>
                        reelscarey
                      </strong>{" "}
                      with folder <strong>ReelScarey</strong>, no sorting and no
                      script, and keep its completed history.
                    </>
                  )}
                </>
              }
            >
              <TextField.Root
                id={`${kind}-download-path`}
                value={downloadPath}
                onChange={(e) => setDownloadPath(e.target.value)}
                placeholder="/downloads"
                required
                maxLength={2048}
                disabled={pending}
              />
            </FormGroup>
          )}
          <div className="rs-form-actions adm-form-indent">
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
            {resource.data.configured && (
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
                  color="red"
                  disabled={pending}
                  onClick={() => void action("remove")}
                >
                  Disconnect
                </Button>
              </>
            )}
          </div>
        </form>
      )}
      {error && <Alert kind="danger">{error}</Alert>}
      {success && <Alert kind="success">{success}</Alert>}
    </section>
  );
}
