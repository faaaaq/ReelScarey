import { useEffect, useState } from "react";

// Dismissed notices are a per-browser preference. "Dismiss" lasts for the
// tab session; "Don't show again" is stored until the owner restores it.
const prefix = "reelscarey-notice:";
const event = "reelscarey:notices";

function read(store: () => Storage, key: string) {
  try {
    return store().getItem(prefix + key) === "hidden";
  } catch {
    return false;
  }
}

export function isNoticeHidden(key: string) {
  return (
    read(() => window.localStorage, key) ||
    read(() => window.sessionStorage, key)
  );
}

export function hideNotice(key: string, permanent: boolean) {
  try {
    (permanent ? window.localStorage : window.sessionStorage).setItem(
      prefix + key,
      "hidden",
    );
  } catch {
    /* Storage can be unavailable; the notice simply returns next time. */
  }
  window.dispatchEvent(new Event(event));
}

export function restoreNotices(keys: string[]) {
  for (const key of keys)
    try {
      window.localStorage.removeItem(prefix + key);
      window.sessionStorage.removeItem(prefix + key);
    } catch {
      /* Nothing stored. */
    }
  window.dispatchEvent(new Event(event));
}

/** Returns which of `keys` are currently hidden, updating on changes. */
export function useHiddenNotices(keys: string[]) {
  const signature = keys.join("|");
  const [hidden, setHidden] = useState(() => keys.filter(isNoticeHidden));
  useEffect(() => {
    const list = signature ? signature.split("|") : [];
    const update = () => setHidden(list.filter(isNoticeHidden));
    update();
    window.addEventListener(event, update);
    return () => window.removeEventListener(event, update);
  }, [signature]);
  return hidden;
}
