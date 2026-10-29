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
      setStatus({ authenticated: false, setup_required: false });
    }
    window.addEventListener("reelscarey:unauthorized", expired);
    return () => window.removeEventListener("reelscarey:unauthorized", expired);
  }, []);
  const signOut = useCallback(async () => {
    await api("/auth/logout", { method: "POST", body: "{}" });
    setCSRFToken("");
    setStatus({ authenticated: false, setup_required: false });
  }, []);
  if (loading || error)
    return (
      <div className="auth-shell">
        <div className="panel auth-card">
          <RequestState
            loading={loading}
            error={error}
            retry={() => setAttempt((n) => n + 1)}
          />
        </div>
      </div>
    );
  if (!status?.authenticated || !status.session)
    return (
      <AuthForm
        setup={status?.setup_required ?? false}
        onSignedIn={(session) => {
          if (status?.setup_required) navigate("/setup", { replace: true });
          setCSRFToken(session.csrf_token);
          setStatus({ authenticated: true, setup_required: false, session });
        }}
      />
    );
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
  const [pending, setPending] = useState(false);
  const [error, setError] = useState("");
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
            ...(setup ? { setup_token: code.trim() } : {}),
          }),
        },
      );
      setPassword("");
      setCode("");
      onSignedIn(session);
    } catch (error) {
      setError(messageFor(error));
      setPassword("");
    } finally {
      setPending(false);
    }
  }
  return (
    <div className="auth-shell">
      <section className="panel auth-card">
        <div className="auth-brand">
          <span className="brand-mark">
            <Film size={23} />
          </span>
          <span className="wordmark">ReelScarey</span>
        </div>
        <h1>{setup ? "Create administrator" : "Sign in"}</h1>
        <p className="muted">
          {setup
            ? "Create the administrator account for this installation."
            : "Sign in to manage your library."}
        </p>
        <form onSubmit={submit}>
          <fieldset disabled={pending}>
            {setup && (
              <>
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
                  size="3"
                >
                  <TextField.Slot>
                    <KeyRound size={16} />
                  </TextField.Slot>
                </TextField.Root>
                <p className="field-help" id="setup-code-help">
                  Copy all 43 characters from the one-time{" "}
                  <code>setup-token</code> file in your config folder. This
                  confirms you own this installation.
                </p>
                <details className="setup-help">
                  <summary>Using Docker?</summary>
                  <p>Run this on the server and enter the code above:</p>
                  <code>
                    docker compose exec reelscarey reelscarey setup-token
                  </code>
                </details>
              </>
            )}
            <label htmlFor="auth-username">Username</label>
            <TextField.Root
              id="auth-username"
              value={username}
              onChange={(e) => setUsername(e.target.value)}
              required
              minLength={setup ? 3 : 1}
              maxLength={40}
              pattern={setup ? "[A-Za-z0-9_-]+" : undefined}
              autoComplete="username"
              autoCapitalize="none"
              spellCheck={false}
              size="3"
            />
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
              size="3"
            />
            {setup && (
              <p className="field-help">
                At least 12 characters. Use a unique password.
              </p>
            )}
          </fieldset>
          {error && (
            <p className="form-error" role="alert">
              {error}
            </p>
          )}
          <Button
            size="3"
            type="submit"
            disabled={pending}
            className="auth-submit"
          >
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
