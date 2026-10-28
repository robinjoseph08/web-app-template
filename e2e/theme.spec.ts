import { expect, test } from "./fixtures";

test("follows the device's color scheme until a choice is made", async ({
  page,
}) => {
  await page.emulateMedia({ colorScheme: "dark" });
  await page.goto("/");
  await expect(page.locator("html")).toHaveClass(/\bdark\b/);

  await page.getByRole("button", { name: "Use light theme" }).click();
  await expect(page.locator("html")).not.toHaveClass(/\bdark\b/);

  // The choice outlasts a reload, before the app's script runs.
  await page.reload();
  await expect(page.locator("html")).not.toHaveClass(/\bdark\b/);
});

test("starts light on a light device", async ({ page }) => {
  await page.emulateMedia({ colorScheme: "light" });
  await page.goto("/");

  await expect(page.locator("html")).not.toHaveClass(/\bdark\b/);
  await expect(
    page.getByRole("button", { name: "Use dark theme" }),
  ).toBeVisible();
});
