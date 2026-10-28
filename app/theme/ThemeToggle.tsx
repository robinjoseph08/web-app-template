import { useEffect, useState } from "react";

import { chooseTheme, currentTheme, followDeviceTheme } from "./theme";

export function ThemeToggle() {
  // index.html has already applied the theme by the time React renders.
  const [theme, setTheme] = useState(currentTheme);

  useEffect(() => followDeviceTheme(setTheme), []);

  const next = theme === "dark" ? "light" : "dark";
  return (
    <button
      aria-label={`Use ${next} theme`}
      className="rounded-full border border-line px-4 py-2 text-sm text-muted transition-colors hover:text-ink"
      onClick={() => {
        chooseTheme(next);
        setTheme(next);
      }}
      type="button"
    >
      {next === "dark" ? "Dark" : "Light"}
    </button>
  );
}
