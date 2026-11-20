import { useCallback, useEffect, useRef, useState } from "react";
import { APIError, api, messageFor } from "./api";

export function useResource<T>(
  path: string | null,
  options?: { retain?: boolean; timeoutMs?: number },
) {
  // `retain` keeps the previous data on the screen while a reload is in
  // flight (background-refresh semantics, like useLiveResource) so callers
  // whose subtree would otherwise remount — and lose local UI state such as
  // action notices — stay stable.
  const retainRef = useRef(options?.retain ?? false);
  retainRef.current = options?.retain ?? false;
  const timeoutRef = useRef(options?.timeoutMs ?? 30000);
  timeoutRef.current = options?.timeoutMs ?? 30000;
  const [data, setData] = useState<T>();
  const [error, setError] = useState("");
  const [code, setCode] = useState("");
  const [loading, setLoading] = useState(true);
  const [attempt, setAttempt] = useState(0);
  const reload = useCallback(() => setAttempt((value) => value + 1), []);

  useEffect(() => {
    if (path === null) {
      setData(undefined);
      setError("");
      setCode("");
      setLoading(false);
      return;
    }
    const controller = new AbortController();
    setLoading(true);
    setError("");
    setCode("");
    setData((current) => (retainRef.current ? current : undefined));
    api<T>(path, {
      // Matches api()'s default read timeout.
      signal: AbortSignal.any([
        controller.signal,
        AbortSignal.timeout(timeoutRef.current),
      ]),
    })
      .then((value) => {
        if (!controller.signal.aborted) setData(value);
      })
      .catch((error) => {
        if (!controller.signal.aborted) {
          setError(messageFor(error));
          setCode(error instanceof APIError ? error.code : "");
        }
      })
      .finally(() => {
        if (!controller.signal.aborted) setLoading(false);
      });
    return () => controller.abort();
  }, [path, attempt]);

  return { data, error, code, loading, reload };
}
