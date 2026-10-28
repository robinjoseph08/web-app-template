import { fireEvent, render, screen } from "@testing-library/react";
import { beforeEach, describe, expect, it, vi } from "vitest";

import {
  applyTheme,
  chooseTheme,
  followDeviceTheme,
  storedTheme,
} from "./theme";
import { ThemeToggle } from "./ThemeToggle";

function stubDevice(dark: boolean) {
  const listeners = new Set<() => void>();
  const query = {
    addEventListener: (_: string, listener: () => void) =>
      listeners.add(listener),
    matches: dark,
    removeEventListener: (_: string, listener: () => void) =>
      listeners.delete(listener),
  };
  vi.stubGlobal("matchMedia", () => query);
  return (nowDark: boolean) => {
    query.matches = nowDark;
    listeners.forEach((listener) => listener());
  };
}

describe("theme", () => {
  beforeEach(() => {
    document.head.innerHTML =
      '<meta name="theme-color" content="#f1f5f9" data-light="#f1f5f9" data-dark="#020617">';
  });

  it("switches the page and the browser's toolbar", () => {
    applyTheme("dark");

    expect(document.documentElement).toHaveClass("dark");
    expect(document.querySelector('meta[name="theme-color"]')).toHaveAttribute(
      "content",
      "#020617",
    );
  });

  it("remembers a choice", () => {
    expect(storedTheme()).toBeNull();

    chooseTheme("light");

    expect(storedTheme()).toBe("light");
    expect(document.documentElement).not.toHaveClass("dark");
  });

  it("follows the device until a choice is made", () => {
    const changeDevice = stubDevice(false);
    const onChange = vi.fn();
    const stop = followDeviceTheme(onChange);

    changeDevice(true);
    expect(document.documentElement).toHaveClass("dark");
    expect(onChange).toHaveBeenLastCalledWith("dark");

    chooseTheme("light");
    changeDevice(true);
    expect(document.documentElement).not.toHaveClass("dark");
    stop();
  });

  it("toggles from the button", () => {
    stubDevice(false);
    render(<ThemeToggle />);

    fireEvent.click(screen.getByRole("button", { name: "Use dark theme" }));

    expect(document.documentElement).toHaveClass("dark");
    expect(storedTheme()).toBe("dark");
    expect(
      screen.getByRole("button", { name: "Use light theme" }),
    ).toBeInTheDocument();
  });
});
