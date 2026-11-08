import {
  createContext,
  useContext,
  useEffect,
  useState,
  type ReactNode,
} from "react";
import { Theme } from "@radix-ui/themes";

type Mode = "dark" | "light";

type ThemeContextValue = {
  mode: Mode;
  setMode: (mode: Mode) => void;
  toggle: () => void;
};

const ThemeContext = createContext<ThemeContextValue>({
  mode: "dark",
  setMode: () => undefined,
  toggle: () => undefined,
});

function initialMode(): Mode {
  try {
    const saved = window.localStorage.getItem("reelscarey-theme");
    if (saved === "dark" || saved === "light") return saved;
  } catch {
    /* Storage can be unavailable in private contexts. */
  }
  if (
    typeof window.matchMedia === "function" &&
    window.matchMedia("(prefers-color-scheme: light)").matches
  )
    return "light";
  return "dark";
}

export function ThemeProvider({ children }: { children: ReactNode }) {
  const [mode, setMode] = useState<Mode>(initialMode);
  useEffect(() => {
    document.documentElement.dataset.theme = mode;
    document.documentElement.style.colorScheme = mode;
    try {
      window.localStorage.setItem("reelscarey-theme", mode);
    } catch {
      /* Keep the selected theme for this session. */
    }
  }, [mode]);
  return (
    <ThemeContext.Provider
      value={{
        mode,
        setMode,
        toggle: () => setMode(mode === "dark" ? "light" : "dark"),
      }}
    >
      <Theme
        appearance={mode}
        accentColor="blue"
        grayColor="gray"
        radius="small"
        panelBackground="solid"
        className="rs-app-theme"
      >
        {children}
      </Theme>
    </ThemeContext.Provider>
  );
}

export function useTheme() {
  return useContext(ThemeContext);
}
