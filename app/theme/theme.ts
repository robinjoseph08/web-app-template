export type Theme = "light" | "dark";

// The inline script in index.html reads the same key before the page draws.
const storageKey = "theme";
const darkQuery = "(prefers-color-scheme: dark)";

// storedTheme is the visitor's choice, or null when they haven't made one (or
// storage is blocked), in which case the device's setting applies.
export function storedTheme(): Theme | null {
  try {
    const value = localStorage.getItem(storageKey);
    return value === "light" || value === "dark" ? value : null;
  } catch {
    return null;
  }
}

export function deviceTheme(): Theme {
  return matchMedia(darkQuery).matches ? "dark" : "light";
}

export function currentTheme(): Theme {
  return document.documentElement.classList.contains("dark") ? "dark" : "light";
}

// applyTheme switches the page's colors and the browser's toolbar color.
export function applyTheme(theme: Theme) {
  document.documentElement.classList.toggle("dark", theme === "dark");
  const meta = document.querySelector<HTMLMetaElement>(
    'meta[name="theme-color"]',
  );
  const color = meta?.dataset[theme];
  if (meta && color) {
    meta.content = color;
  }
}

// chooseTheme saves the visitor's choice, which then wins over the device's
// setting on every visit.
export function chooseTheme(theme: Theme) {
  try {
    localStorage.setItem(storageKey, theme);
  } catch {
    // Storage is blocked, so the choice lasts for this page only.
  }
  applyTheme(theme);
}

// followDeviceTheme keeps the page in step with the device's setting while the
// visitor hasn't made a choice, and returns a function that stops it.
export function followDeviceTheme(onChange: (theme: Theme) => void) {
  const query = matchMedia(darkQuery);
  const listener = () => {
    if (storedTheme() === null) {
      const theme = deviceTheme();
      applyTheme(theme);
      onChange(theme);
    }
  };
  query.addEventListener("change", listener);
  return () => query.removeEventListener("change", listener);
}
