import { expect, test } from "./fixtures";

test("shows the application introduction", async ({ page }) => {
  await page.goto("/");

  await expect(
    page.getByRole("heading", {
      level: 1,
      name: "Your application starts here.",
    }),
  ).toBeVisible();
});

test("shows the version the server reports", async ({ page, request }) => {
  const response = await request.get("/api/version");
  expect(response.ok()).toBe(true);
  const { version } = await response.json();

  await page.goto("/");

  await expect(page.getByText(`Version ${version}`)).toBeVisible();
});

test("renders client routes from the app shell", async ({ page }) => {
  const response = await page.goto("/settings/profile");

  expect(response?.status()).toBe(200);
  await expect(page.getByRole("heading", { level: 1 })).toBeVisible();
});

test("serves built assets compressed and cached forever", async ({
  page,
  request,
}) => {
  await page.goto("/");
  const script = await page
    .locator('script[type="module"][src^="/assets/"]')
    .getAttribute("src");
  expect(script).not.toBeNull();

  const response = await request.get(script!, {
    headers: { "Accept-Encoding": "gzip" },
  });

  expect(response.ok()).toBe(true);
  expect(response.headers()["content-encoding"]).toBe("gzip");
  expect(response.headers()["cache-control"]).toBe(
    "public, max-age=31536000, immutable",
  );
});
