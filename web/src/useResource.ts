import { useCallback, useEffect, useState } from "react";
import { APIError, api, messageFor } from "./api";

export function useResource<T>(path: string | null) {
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
    setData(undefined);
    api<T>(path, {
      signal: AbortSignal.any([controller.signal, AbortSignal.timeout(15000)]),
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
