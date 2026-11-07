import { useCallback, useRef, useState } from "react";

export type Selection = {
  selected: ReadonlySet<string>;
  anchor: string | null;
  focus: string | null;
};

export type Modifiers = { ctrl: boolean; shift: boolean };

const empty: Selection = { selected: new Set(), anchor: null, focus: null };

/**
 * qBittorrent-style row selection keyed by info hash, so it survives list
 * refreshes and re-sorting. `order` must be the currently visible hashes.
 */
export function useSelection(order: string[]) {
  const [state, setState] = useState<Selection>(empty);
  const orderRef = useRef(order);
  orderRef.current = order;

  const range = useCallback((from: string | null, to: string) => {
    const list = orderRef.current;
    const a = from ? list.indexOf(from) : -1;
    const b = list.indexOf(to);
    if (b < 0) return [to];
    if (a < 0) return [to];
    const [lo, hi] = a < b ? [a, b] : [b, a];
    return list.slice(lo, hi + 1);
  }, []);

  const click = useCallback(
    (hash: string, mods: Modifiers) => {
      setState((s) => {
        if (mods.shift) {
          const span = range(s.anchor, hash);
          const selected = new Set(mods.ctrl ? s.selected : []);
          for (const h of span) selected.add(h);
          return { selected, anchor: s.anchor ?? hash, focus: hash };
        }
        if (mods.ctrl) {
          const selected = new Set(s.selected);
          if (selected.has(hash)) selected.delete(hash);
          else selected.add(hash);
          return { selected, anchor: hash, focus: hash };
        }
        return { selected: new Set([hash]), anchor: hash, focus: hash };
      });
    },
    [range],
  );

  /** Right click keeps a multi-selection that already contains the row. */
  const context = useCallback((hash: string) => {
    setState((s) =>
      s.selected.has(hash)
        ? { ...s, focus: hash }
        : { selected: new Set([hash]), anchor: hash, focus: hash },
    );
  }, []);

  const move = useCallback(
    (delta: number | "start" | "end", mods: Modifiers) => {
      setState((s) => {
        const list = orderRef.current;
        if (!list.length) return s;
        const current = s.focus ? list.indexOf(s.focus) : -1;
        let index: number;
        if (delta === "start") index = 0;
        else if (delta === "end") index = list.length - 1;
        else if (current < 0) index = delta > 0 ? 0 : list.length - 1;
        else index = Math.max(0, Math.min(list.length - 1, current + delta));
        const hash = list[index];
        if (mods.shift) {
          const anchor = s.anchor ?? s.focus ?? hash;
          return {
            selected: new Set(range(anchor, hash)),
            anchor,
            focus: hash,
          };
        }
        if (mods.ctrl) return { ...s, focus: hash };
        return { selected: new Set([hash]), anchor: hash, focus: hash };
      });
    },
    [range],
  );

  const toggleFocus = useCallback(() => {
    setState((s) => {
      if (!s.focus) return s;
      const selected = new Set(s.selected);
      if (selected.has(s.focus)) selected.delete(s.focus);
      else selected.add(s.focus);
      return { ...s, selected, anchor: s.focus };
    });
  }, []);

  const selectAll = useCallback(() => {
    setState((s) => ({
      selected: new Set(orderRef.current),
      anchor: s.anchor,
      focus: s.focus ?? orderRef.current[0] ?? null,
    }));
  }, []);

  const clear = useCallback(() => setState(empty), []);

  return { ...state, click, context, move, toggleFocus, selectAll, clear };
}
