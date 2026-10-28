import { render, screen } from "@testing-library/react";
import { describe, expect, it, vi } from "vitest";

import { App } from "./App";

describe("App", () => {
  it("shows the application introduction", () => {
    vi.stubGlobal("fetch", () => new Promise(() => {}));
    render(<App />);

    expect(
      screen.getByRole("heading", {
        level: 1,
        name: "Your application starts here.",
      }),
    ).toBeInTheDocument();
  });

  it("shows the version the server reports", async () => {
    vi.stubGlobal("fetch", async () => Response.json({ version: "v1.2.3" }));
    render(<App />);

    expect(await screen.findByText("Version v1.2.3")).toBeInTheDocument();
  });

  it("leaves the version out when the server doesn't answer", async () => {
    const fetch = vi.fn(async () => {
      throw new TypeError("Failed to fetch");
    });
    vi.stubGlobal("fetch", fetch);
    render(<App />);

    await vi.waitFor(() => expect(fetch).toHaveBeenCalled());
    expect(screen.queryByText(/^Version/)).not.toBeInTheDocument();
  });
});
