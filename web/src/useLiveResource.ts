import { useCallback, useEffect, useRef, useState } from "react";
import { APIError, api, messageFor } from "./api";
// Background refreshes retain drafts and the last useful response on outages.
export function useLiveResource<T>(path: string | null, interval = 0) {
  const [data, setData] = useState<T>();
  const [error, setError] = useState("");
  const [code, setCode] = useState("");
  const [loading, setLoading] = useState(true);
  const [updatedAt, setUpdatedAt] = useState<number>();
  const [attempt, setAttempt] = useState(0);
  const currentPath = useRef<string | null>(null);
  const reload = useCallback(() => setAttempt((n) => n + 1), []);
  useEffect(() => {
    if (path === null) {
      currentPath.current = null;
      setData(undefined);
      setError("");
      setCode("");
      setUpdatedAt(undefined);
      setLoading(false);
      return;
    }
    const controller = new AbortController();
    let busy = false;
    // A different URL is new content: clear stale rows first. Automatic
    // refreshes of the same URL keep the last good response visible.
    if (currentPath.current !== path) {
      currentPath.current = path;
      setData(undefined);
      setError("");
      setCode("");
      setUpdatedAt(undefined);
      setLoading(true);
    }
    async function read() {
      if (busy) return;
      busy = true;
      try {
        const value = await api<T>(path as string, {
          signal: AbortSignal.any([
            controller.signal,
            AbortSignal.timeout(30000),
          ]),
        });
        if (!controller.signal.aborted) {
          setData(value);
          setError("");
          setCode("");
          setUpdatedAt(Date.now());
        }
      } catch (e) {
        if (!controller.signal.aborted) {
          setError(messageFor(e));
          setCode(e instanceof APIError ? e.code : "");
        }
      } finally {
        busy = false;
        if (!controller.signal.aborted) setLoading(false);
      }
    }
    void read();
    function refreshVisible() {
      if (document.visibilityState !== "hidden") void read();
    }
    const timer = interval
      ? window.setInterval(refreshVisible, interval)
      : undefined;
    if (interval) document.addEventListener("visibilitychange", refreshVisible);
    return () => {
      controller.abort();
      if (timer) window.clearInterval(timer);
      if (interval)
        document.removeEventListener("visibilitychange", refreshVisible);
    };
  }, [path, interval, attempt]);
  return { data, error, code, loading, updatedAt, reload };
}
