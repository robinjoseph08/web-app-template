import { test as base, expect } from "@playwright/test";

// WebKit reports a request that a navigation cancels (the test moving to
// another page while the app is still fetching) as a page error worded like a
// cross-origin failure. The request belonged to the page being left, so it's
// not a real error. The site and its API share an origin, so a same-origin
// request can't actually fail access control.
export function cancelledByNavigation(error: Error, host: string): boolean {
  const text = `${error.name}: ${error.message}`;
  return (
    text.startsWith("Fetch API cannot load") &&
    text.includes(host) &&
    text.endsWith("due to access control checks.")
  );
}

// Every test runs against the local server only. Anything else the site loads
// (fonts, analytics) is blocked, so tests don't depend on the network or
// report to real services. Uncaught errors on the page fail the test.
export const test = base.extend({
  page: async ({ baseURL, page }, use) => {
    const { host, origin } = new URL(baseURL!);
    await page.route(
      (url) => url.origin !== origin,
      (route) => route.abort(),
    );

    const errors: Error[] = [];
    page.on("pageerror", (error) => {
      if (!cancelledByNavigation(error, host)) {
        errors.push(error);
      }
    });
    await use(page);
    expect(errors, "uncaught errors on the page").toEqual([]);
  },
});

export { expect };
