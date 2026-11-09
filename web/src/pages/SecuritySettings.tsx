import { useState, type FormEvent } from "react";
import { Button, TextField } from "@radix-ui/themes";
import { Copy, KeyRound, ShieldCheck, ShieldOff } from "lucide-react";
import { api, messageFor, type TOTPProvision, type TOTPStatus } from "../api";
import { PageHeading, RequestState } from "../components";
import { Alert, FieldSet, FormGroup, PageBody } from "../ui";
import { useResource } from "../useResource";
import "./Admin.css";

type ConfirmResult = { enabled: true; recovery_codes: string[] };

function copyText(value: string) {
  if (!navigator.clipboard?.writeText)
    return Promise.reject(new Error("Clipboard unavailable"));
  return navigator.clipboard.writeText(value);
}

export function SecuritySettings() {
  const totp = useResource<TOTPStatus>("/auth/totp");
  const [provision, setProvision] = useState<TOTPProvision>();
  const [recoveryCodes, setRecoveryCodes] = useState<string[]>();
  const [code, setCode] = useState("");
  const [busy, setBusy] = useState(false);
  const [error, setError] = useState("");
  const [notice, setNotice] = useState("");
  const [copied, setCopied] = useState("");

  function resetMessages() {
    setError("");
    setNotice("");
    setCopied("");
  }

  async function startSetup() {
    setBusy(true);
    resetMessages();
    try {
      setProvision(
        await api<TOTPProvision>("/auth/totp/provision", {
          method: "POST",
          body: "{}",
        }),
      );
      setRecoveryCodes(undefined);
      setCode("");
    } catch (e) {
      setError(messageFor(e));
    } finally {
      setBusy(false);
    }
  }

  async function confirm(event: FormEvent) {
    event.preventDefault();
    if (busy || !provision) return;
    setBusy(true);
    resetMessages();
    try {
      const result = await api<ConfirmResult>("/auth/totp/confirm", {
        method: "POST",
        body: JSON.stringify({ code: code.trim() }),
      });
      setRecoveryCodes(result.recovery_codes);
      setProvision(undefined);
      setCode("");
      setNotice("Two-factor authentication is enabled.");
      totp.reload();
    } catch (e) {
      setError(messageFor(e));
    } finally {
      setBusy(false);
    }
  }

  async function disable(event: FormEvent) {
    event.preventDefault();
    if (busy) return;
    setBusy(true);
    resetMessages();
    try {
      await api("/auth/totp/disable", {
        method: "POST",
        body: JSON.stringify({ code: code.trim() }),
      });
      setCode("");
      setRecoveryCodes(undefined);
      setNotice(
        "Two-factor authentication is disabled. The saved secret and unused recovery codes were removed.",
      );
      totp.reload();
    } catch (e) {
      setError(messageFor(e));
    } finally {
      setBusy(false);
    }
  }

  async function copy(label: string, value: string) {
    resetMessages();
    try {
      await copyText(value);
      setCopied(`${label} copied.`);
    } catch {
      setError(
        `Could not copy automatically. Select the ${label.toLocaleLowerCase()} and copy it manually.`,
      );
    }
  }

  const status = totp.data;
  return (
    <PageBody>
      <PageHeading
        title="Security"
        description="Protect administrator sign-in with an authenticator app. Two-factor authentication is optional and applies to this administrator account only."
      />
      <RequestState
        loading={totp.loading}
        error={totp.error}
        retry={totp.reload}
      />
      {notice && <Alert kind="success">{notice}</Alert>}
      {error && (
        <Alert kind="danger" role="alert">
          {error}
        </Alert>
      )}
      {copied && (
        <Alert kind="info" role="status">
          {copied}
        </Alert>
      )}
      {recoveryCodes && (
        <FieldSet
          legend="Save your recovery codes"
          description="These codes are shown only once. They replace the authenticator code when the app is unavailable. Each code works once. Store them somewhere private; ReelScarey keeps only hashes."
          actions={
            <Button
              size="1"
              variant="soft"
              onClick={() =>
                void copy("Recovery codes", recoveryCodes.join("\n"))
              }
            >
              <Copy size={14} aria-hidden="true" />
              Copy codes
            </Button>
          }
        >
          <pre className="sec-recovery" aria-label="Recovery codes">
            {recoveryCodes.join("\n")}
          </pre>
          <Button variant="soft" onClick={() => setRecoveryCodes(undefined)}>
            I saved these codes
          </Button>
        </FieldSet>
      )}
      {status && !recoveryCodes && (
        <>
          <FieldSet legend="Authenticator app">
            {status.enabled ? (
              <>
                <p>
                  Two-factor authentication is <strong>enabled</strong>. Signing
                  in requires your password and a current authenticator code or
                  an unused recovery code.
                </p>
                <p className="rs-muted">
                  {status.recovery_codes_remaining === 1
                    ? "1 unused recovery code remains."
                    : `${status.recovery_codes_remaining} unused recovery codes remain.`}
                </p>
              </>
            ) : (
              <p>
                Two-factor authentication is <strong>not enabled</strong>. It is
                optional; your password alone still signs in. Enabling it adds a
                six-digit code from an authenticator app.
                {status.configured
                  ? " Setup was started earlier but not confirmed. Starting again replaces the unconfirmed secret."
                  : ""}
              </p>
            )}
          </FieldSet>
          {!status.enabled && !provision && (
            <FieldSet
              legend="Set up two-factor authentication"
              description="Start setup to generate a secret for your authenticator app. The secret is shown once and is not saved in this browser."
            >
              <Button disabled={busy} onClick={() => void startSetup()}>
                <ShieldCheck size={16} aria-hidden="true" />
                {busy
                  ? "Starting…"
                  : status.configured
                    ? "Start setup again"
                    : "Start setup"}
              </Button>
            </FieldSet>
          )}
          {!status.enabled && provision && (
            <>
              <FieldSet
                legend="1. Add the secret to your authenticator app"
                description="Open your authenticator app, choose to add an account manually, and enter this secret. You can also copy the setup URI if your app accepts one."
                actions={
                  <Button
                    size="1"
                    variant="soft"
                    onClick={() => void copy("Secret", provision.secret)}
                  >
                    <Copy size={14} aria-hidden="true" />
                    Copy secret
                  </Button>
                }
              >
                <p className="sec-secret" aria-label="Authenticator secret">
                  {provision.secret}
                </p>
                <p className="rs-muted rs-small sec-uri">
                  Setup URI: <code>{provision.otpauth_uri}</code>
                </p>
              </FieldSet>
              <FieldSet legend="2. Confirm a code">
                <form className="rs-stack" onSubmit={confirm}>
                  <FormGroup
                    label="Authenticator code"
                    htmlFor="totp-confirm-code"
                    help="Enter the current six-digit code from the authenticator app. If you leave this page, the unconfirmed secret is not shown again; start setup again to replace it."
                  >
                    <TextField.Root
                      id="totp-confirm-code"
                      value={code}
                      onChange={(e) => {
                        setCode(e.target.value);
                        setError("");
                      }}
                      inputMode="numeric"
                      autoComplete="one-time-code"
                      maxLength={32}
                      required
                    >
                      <TextField.Slot>
                        <KeyRound size={16} />
                      </TextField.Slot>
                    </TextField.Root>
                  </FormGroup>
                  <Button type="submit" disabled={busy || !code.trim()}>
                    <ShieldCheck size={16} aria-hidden="true" />
                    {busy ? "Confirming…" : "Confirm and enable"}
                  </Button>
                </form>
              </FieldSet>
            </>
          )}
          {status.enabled && (
            <FieldSet
              legend="Disable two-factor authentication"
              description="Disabling removes the stored secret and all unused recovery codes. Your password alone will sign in again."
            >
              <form className="rs-stack" onSubmit={disable}>
                <FormGroup
                  label="Authenticator or recovery code"
                  htmlFor="totp-disable-code"
                  help="Confirm with a current authenticator code or one unused recovery code."
                >
                  <TextField.Root
                    id="totp-disable-code"
                    value={code}
                    onChange={(e) => {
                      setCode(e.target.value);
                      setError("");
                    }}
                    autoComplete="one-time-code"
                    maxLength={64}
                    required
                  >
                    <TextField.Slot>
                      <ShieldOff size={16} />
                    </TextField.Slot>
                  </TextField.Root>
                </FormGroup>
                <Button
                  type="submit"
                  color="red"
                  disabled={busy || !code.trim()}
                >
                  {busy ? "Disabling…" : "Disable two-factor authentication"}
                </Button>
              </form>
            </FieldSet>
          )}
        </>
      )}
      <FieldSet legend="Sign-in details">
        <p className="rs-muted">
          Two-factor authentication protects the administrator account. Request
          accounts have their own separate sessions and do not use this secret.
        </p>
        {status?.enabled && (
          <p className="rs-muted rs-small">
            If you lose both the authenticator app and your recovery codes,
            signing in requires restoring a backup or an offline database
            change. No passkey or alternative sign-in method is configured.
          </p>
        )}
      </FieldSet>
    </PageBody>
  );
}
