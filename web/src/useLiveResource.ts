import { useCallback, useEffect, useState } from "react";
import { api, messageFor } from "./api";
// Background refreshes retain drafts and the last useful response on outages.
export function useLiveResource<T>(path: string, interval = 0) {
  const [data, setData] = useState<T>();
  const [error, setError] = useState("");
  const [loading, setLoading] = useState(true);
  const [attempt, setAttempt] = useState(0);
  const reload = useCallback(() => setAttempt((n) => n + 1), []);
  useEffect(() => {
    const controller = new AbortController();
    let busy = false;
    async function read() {
      if (busy) return;
      busy = true;
      try {
        const value = await api<T>(path, {
          signal: AbortSignal.any([
            controller.signal,
            AbortSignal.timeout(15000),
          ]),
        });
        if (!controller.signal.aborted) {
          setData(value);
          setError("");
        }
      } catch (e) {
        if (!controller.signal.aborted) setError(messageFor(e));
      } finally {
        busy = false;
        if (!controller.signal.aborted) setLoading(false);
      }
    }
    void read();
    const timer = interval
      ? window.setInterval(() => void read(), interval)
      : undefined;
    return () => {
      controller.abort();
      if (timer) window.clearInterval(timer);
    };
  }, [path, interval, attempt]);
  return { data, error, loading, reload };
}
