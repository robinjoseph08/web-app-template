import "@testing-library/jest-dom/vitest";

import { cleanup } from "@testing-library/react";
import { afterEach, beforeEach, vi } from "vitest";

// Node's own localStorage global hides jsdom's, and without a backing file it
// has no methods, so every test gets an empty in-memory one instead.
class MemoryStorage implements Storage {
  #items = new Map<string, string>();

  get length() {
    return this.#items.size;
  }

  clear() {
    this.#items.clear();
  }

  getItem(key: string) {
    return this.#items.get(key) ?? null;
  }

  key(index: number) {
    return [...this.#items.keys()][index] ?? null;
  }

  removeItem(key: string) {
    this.#items.delete(key);
  }

  setItem(key: string, value: string) {
    this.#items.set(key, String(value));
  }
}

// Testing Library only unmounts on its own when Vitest runs with globals.
afterEach(cleanup);

beforeEach(() => {
  vi.unstubAllGlobals();
  vi.stubGlobal("localStorage", new MemoryStorage());
  // jsdom has no layout engine, so media queries never match. Tests that care
  // about one, like the device's color scheme, stub it themselves.
  // Responsive behavior is exercised in Playwright.
  vi.stubGlobal("matchMedia", (media: string) => ({
    addEventListener: () => {},
    matches: false,
    media,
    removeEventListener: () => {},
  }));
  document.documentElement.className = "";
});
