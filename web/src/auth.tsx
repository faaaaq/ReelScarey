import {
  createContext,
  useCallback,
  useContext,
  useEffect,
  useState,
  type ReactNode,
  type FormEvent,
} from "react";
import { Button, TextField } from "@radix-ui/themes";
import { Film, KeyRound, LogIn, ShieldCheck } from "lucide-react";
import {
  api,
  APIError,
  messageFor,
  setCSRFToken,
  type AuthStatus,
  type Session,
} from "./api";
import { RequestState } from "./components";
import { useNavigate } from "react-router-dom";

const AuthContext = createContext<{
  session: Session;
  signOut: () => Promise<void>;
} | null>(null);
export function useAuth() {
  const value = useContext(AuthContext);
  if (!value) throw new Error("Authentication context missing");
  return value;
}

export function AuthGate({ children }: { children: ReactNode }) {
  const navigate = useNavigate();
  const [status, setStatus] = useState<AuthStatus>();
  const [loading, setLoading] = useState(true);
  const [error, setError] = useState("");
  const [attempt, setAttempt] = useState(0);
  // Last known session, kept separately from `status` so the app can stay
  // mounted (preserving in-progress work) while a re-authentication overlay
  // is shown after a 401.
  const [session, setSession] = useState<Session>();
  // Whether an authenticated session was ever established in this page load.
  // Once true, a later 401 shows the sign-in form as an overlay instead of
  // replacing the whole app, which would destroy unsaved work.
  const [hadSession, setHadSession] = useState(false);
  useEffect(() => {
    const controller = new AbortController();
    setLoading(true);
    setError("");
    api<AuthStatus>("/auth/session", {
      signal: AbortSignal.any([controller.signal, AbortSignal.timeout(15000)]),
    })
      .then((result) => {
        if (!controller.signal.aborted) {
          setCSRFToken(result.session?.csrf_token ?? "");
          if (result.authenticated && result.session) {
            setSession(result.session);
            setHadSession(true);
          }
          setStatus(result);
        }
      })
      .catch((error) => {
        if (!controller.signal.aborted) setError(messageFor(error));
      })
      .finally(() => {
        if (!controller.signal.aborted) setLoading(false);
      });
    return () => controller.abort();
  }, [attempt]);
  useEffect(() => {
    function expired() {
      setCSRFToken("");
      // Only clear the status when a session was actually active; repeated
      // 401s while the re-authentication overlay is already up must not
      // re-render the whole tree.
      setStatus((prev) =>
        prev?.authenticated
          ? { authenticated: false, setup_required: false }
          : prev,
      );
    }
    window.addEventListener("reelscarey:unauthorized", expired);
    return () => window.removeEventListener("reelscarey:unauthorized", expired);
  }, []);
  const signOut = useCallback(async () => {
    await api("/auth/logout", { method: "POST", body: "{}" });
    setCSRFToken("");
    setSession(undefined);
    setHadSession(false);
    setStatus({ authenticated: false, setup_required: false });
  }, []);
  if (loading || error)
    return (
      <div className="rs-auth">
        <div className="rs-auth-card">
          <div className="rs-auth-body">
            <RequestState
              loading={loading}
              error={error}
              retry={() => setAttempt((n) => n + 1)}
            />
          </div>
        </div>
      </div>
    );
  if (!status?.authenticated || !status.session) {
    const form = (
      <AuthForm
        setup={status?.setup_required ?? false}
        onSignedIn={(next) => {
          if (status?.setup_required) navigate("/setup", { replace: true });
          setCSRFToken(next.csrf_token);
          setSession(next);
          setHadSession(true);
          setStatus({
            authenticated: true,
            setup_required: false,
            session: next,
          });
        }}
      />
    );
    if (hadSession && session) {
      // The session expired mid-use. Keep the app mounted underneath so
      // unsaved work (forms, drafts, pending edits) survives, and collect a
      // fresh sign-in on top. Authentication is not weakened: every API call
      // still fails with 401 until the user signs in again, and no route
      // change happens behind the user's back.
      return (
        <AuthContext.Provider value={{ session, signOut }}>
          {children}
          <div
            className="rs-auth rs-auth-overlay"
            role="dialog"
            aria-modal="true"
            aria-label="Session expired"
            style={{
              position: "fixed",
              inset: 0,
              zIndex: 1000,
            }}
          >
            {form}
          </div>
        </AuthContext.Provider>
      );
    }
    return form;
  }
  return (
    <AuthContext.Provider value={{ session: status.session, signOut }}>
      {children}
    </AuthContext.Provider>
  );
}

function AuthForm({
  setup,
  onSignedIn,
}: {
  setup: boolean;
  onSignedIn: (session: Session) => void;
}) {
  useEffect(() => {
    document.title = `${setup ? "Create administrator" : "Sign in"} · ReelScarey`;
  }, [setup]);
  const [username, setUsername] = useState("");
  const [password, setPassword] = useState("");
  const [code, setCode] = useState("");
  const [importMode, setImportMode] = useState<"hardlink" | "copy">("hardlink");
  const [pending, setPending] = useState(false);
  const [error, setError] = useState("");
  // Two-factor status is deliberately not revealed before authentication.
  // The code field appears after a sign-in that used the right credentials
  // and was asked for a code.
  const [totpRequired, setTotpRequired] = useState(false);
  async function submit(event: FormEvent) {
    event.preventDefault();
    if (pending) return;
    if (setup && code.trim().length !== 43) {
      setError(
        "The setup code must contain all 43 characters. Copy the entire line from setup-token.",
      );
      return;
    }
    setPending(true);
    setError("");
    try {
      const session = await api<Session>(
        setup ? "/auth/setup" : "/auth/login",
        {
          method: "POST",
          body: JSON.stringify({
            username,
            password,
            ...(setup
              ? { setup_token: code.trim(), import_mode: importMode }
              : totpRequired
                ? { totp_code: code.trim() }
                : {}),
          }),
        },
      );
      setPassword("");
      setCode("");
      onSignedIn(session);
    } catch (error) {
      setError(messageFor(error));
      if (
        !setup &&
        error instanceof APIError &&
        error.code === "invalid_totp"
      ) {
        setTotpRequired(true);
      }
      setPassword("");
      if (!setup) setCode("");
    } finally {
      setPending(false);
    }
  }
  return (
    <div className="rs-auth">
      <section className="rs-auth-card">
        <div className="rs-auth-head">
          <span className="rs-logo-mark">
            <Film size={18} />
          </span>
          <span>ReelScarey</span>
        </div>
        <form onSubmit={submit} className="rs-auth-body">
          <h1>{setup ? "Create administrator" : "Sign in"}</h1>
          <fieldset disabled={pending} className="rs-stack">
            {setup && (
              <div className="rs-auth-field">
                <label htmlFor="setup-code">Setup code</label>
                <TextField.Root
                  id="setup-code"
                  type="password"
                  value={code}
                  onChange={(e) => {
                    setCode(e.target.value);
                    setError("");
                  }}
                  required
                  maxLength={128}
                  aria-describedby="setup-code-help"
                  autoComplete="off"
                  size="2"
                >
                  <TextField.Slot>
                    <KeyRound size={16} />
                  </TextField.Slot>
                </TextField.Root>
                <p className="rs-form-help" id="setup-code-help">
                  Copy all 43 characters from the one-time{" "}
                  <code>setup-token</code> file in your config folder. This
                  confirms you own this installation.
                </p>
                <details className="rs-small">
                  <summary>Using Docker?</summary>
                  <p className="rs-muted" style={{ margin: "6px 0" }}>
                    Run this on the server and enter the code above:
                  </p>
                  <code>
                    docker compose exec reelscarey reelscarey setup-token
                  </code>
                </details>
              </div>
            )}
            <div className="rs-auth-field">
              <label htmlFor="auth-username">Username</label>
              <TextField.Root
                id="auth-username"
                value={username}
                onChange={(e) => setUsername(e.target.value)}
                required
                minLength={setup ? 3 : 1}
                maxLength={40}
                pattern={setup ? "[A-Za-z0-9_\\-]+" : undefined}
                autoComplete="username"
                autoCapitalize="none"
                spellCheck={false}
                size="2"
              />
            </div>
            <div className="rs-auth-field">
              <label htmlFor="auth-password">Password</label>
              <TextField.Root
                id="auth-password"
                type="password"
                value={password}
                onChange={(e) => setPassword(e.target.value)}
                required
                minLength={setup ? 12 : 1}
                maxLength={256}
                autoComplete={setup ? "new-password" : "current-password"}
                size="2"
              />
              {setup && (
                <p className="rs-form-help">
                  At least 12 characters. Use a unique password.
                </p>
              )}
            </div>
            {!setup && totpRequired && (
              <div className="rs-auth-field">
                <label htmlFor="auth-totp">
                  Authenticator or recovery code
                </label>
                <TextField.Root
                  id="auth-totp"
                  value={code}
                  onChange={(e) => {
                    setCode(e.target.value);
                    setError("");
                  }}
                  required
                  maxLength={64}
                  autoComplete="one-time-code"
                  spellCheck={false}
                  size="2"
                >
                  <TextField.Slot>
                    <ShieldCheck size={16} />
                  </TextField.Slot>
                </TextField.Root>
                <p className="rs-form-help">
                  Enter the current six-digit code from your authenticator app,
                  or one unused recovery code.
                </p>
              </div>
            )}
            {setup && (
              <div className="rs-auth-field">
                <label htmlFor="auth-import-mode">Import method</label>
                <select
                  id="auth-import-mode"
                  value={importMode}
                  onChange={(e) =>
                    setImportMode(e.target.value as "hardlink" | "copy")
                  }
                >
                  <option value="hardlink">Hardlink when possible</option>
                  <option value="copy">Always copy</option>
                </select>
                <p className="rs-form-help">
                  Hardlinks save space only when downloads and the library are
                  on the same writable mounted filesystem; otherwise ReelScarey
                  writes a verified copy. You can change this later in Settings.
                  Symbolic links are not used for library imports.
                </p>
              </div>
            )}
          </fieldset>
          {error && (
            <p className="rs-form-error" role="alert">
              {error}
            </p>
          )}
          <Button size="2" type="submit" disabled={pending}>
            {setup ? <ShieldCheck size={18} /> : <LogIn size={18} />}
            {pending
              ? "Please wait…"
              : setup
                ? "Create administrator"
                : "Sign in"}
          </Button>
        </form>
      </section>
    </div>
  );
}
