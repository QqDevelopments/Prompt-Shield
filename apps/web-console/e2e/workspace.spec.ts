import { expect, test } from "@playwright/test";
test("preferences survive unavailable storage and follow system appearance", async ({
  page,
}) => {
  await page.emulateMedia({ colorScheme: "dark" });
  await page.addInitScript(() => {
    Object.defineProperty(window, "localStorage", {
      get() {
        throw new DOMException("Blocked", "SecurityError");
      },
    });
  });
  await page.goto("/");
  await expect(page.locator("html")).toHaveAttribute("data-theme", "dark");
  await page.getByLabel("Language", { exact: true }).selectOption("tr");
  await expect(page.locator("html")).toHaveAttribute("lang", "tr");
  await expect(page.locator("html")).toHaveAttribute("dir", "ltr");
  await page.emulateMedia({ colorScheme: "light" });
  await expect(page.locator("html")).toHaveAttribute("data-theme", "light");
  await page.getByLabel("Görünüm", { exact: true }).selectOption("dark");
  await page.emulateMedia({ colorScheme: "dark" });
  await page.emulateMedia({ colorScheme: "light" });
  await expect(page.locator("html")).toHaveAttribute("data-theme", "dark");
});

test("saved preferences apply before the application module and invalid values recover", async ({
  page,
}) => {
  await page.addInitScript(() => {
    localStorage.setItem("ps.theme.v1", "dark");
    localStorage.setItem("ps.locale.v1", "invalid");
  });
  await page.route("**/assets/*.js", (route) => route.abort());
  await page.goto("/");
  await expect(page.locator("html")).toHaveAttribute("data-theme", "dark");
  await page.unroute("**/assets/*.js");
  await page.reload();
  await expect(page.getByLabel("Language", { exact: true })).toHaveValue("en");
});
test("real services: inspect, change policy, manage keys, and preserve preferences", async ({
  page,
}) => {
  await page.goto("/admin");
  await page.getByLabel("Language", { exact: true }).selectOption("en");
  await page
    .getByLabel("Operator password")
    .fill("synthetic-e2e-operator-password");
  await page.getByRole("button", { name: "Open workspace" }).click();
  await expect(page.getByText("Engine connected")).toBeVisible();
  await page.getByRole("link", { name: "Inspection lab", exact: true }).click();
  await page.locator(".skip-link").focus();
  await page.locator(".skip-link").press("Enter");
  await expect(page.locator("#main-content")).toBeFocused();
  await expect(page).toHaveURL(/#playground$/);
  await page
    .getByLabel("Your prompt", { exact: true })
    .fill("Email alice@example.com about 4111 1111 1111 1111.");
  await page.getByLabel("Appearance", { exact: true }).selectOption("dark");
  await page.getByLabel("Language", { exact: true }).selectOption("tr");
  await expect(page.locator("html")).toHaveAttribute("lang", "tr");
  await expect(page.locator("html")).toHaveAttribute("data-theme", "dark");
  await expect(page.getByLabel("İstemin", { exact: true })).toHaveValue(
    "Email alice@example.com about 4111 1111 1111 1111.",
  );
  await page
    .getByRole("button", { name: "İstemi denetle", exact: true })
    .click();
  await expect(page.getByText("2 tespit")).toBeVisible();
  await expect(page.locator(".result pre")).not.toContainText(
    "alice@example.com",
  );
  await page.evaluate(() => window.scrollTo(0, 0));
  await expect.poll(() => page.evaluate(() => window.scrollY)).toBe(0);
  await page.screenshot({
    path: "../../output/playwright/inspection-dark-tr.png",
    fullPage: true,
  });
  await page.reload();
  await expect(page.locator("html")).toHaveAttribute("data-theme", "dark");
  await expect(page.getByLabel("İstemin", { exact: true })).toHaveValue("");
  await page.getByLabel("Dil", { exact: true }).selectOption("en");
  await page
    .getByRole("link", { name: "Security policy", exact: true })
    .click();
  await page
    .getByLabel("Blocked keywords", { exact: true })
    .fill("confidential-project");
  await page.getByRole("button", { name: "Save changes" }).click();
  await expect(page.getByText("Changes saved")).toBeVisible();
  await page.getByRole("link", { name: "Inspection lab", exact: true }).click();
  await page
    .getByLabel("Your prompt", { exact: true })
    .fill("confidential-project");
  await page
    .getByRole("button", { name: "Inspect prompt", exact: true })
    .click();
  await expect(
    page.getByText(
      "This request would be stopped before reaching the provider.",
    ),
  ).toBeVisible();
  await page.getByRole("link", { name: "Activity", exact: true }).click();
  await page.getByLabel("Status", { exact: true }).selectOption("masked");
  await expect(
    page.getByRole("cell", { name: "Masked", exact: true }).first(),
  ).toBeVisible();
  await expect(
    page.getByRole("cell", { name: "Blocked", exact: true }),
  ).toHaveCount(0);
  await page.getByRole("link", { name: "API keys", exact: true }).click();
  await page.getByLabel("Name", { exact: true }).fill("Browser test");
  await page.getByRole("button", { name: "Create API key" }).click();
  await expect(
    page.getByText("Save this key now. It will not be shown again."),
  ).toBeVisible();
  await page.getByRole("button", { name: "I saved the key" }).click();
  await page.getByRole("button", { name: "Revoke", exact: true }).click();
  await page
    .getByRole("dialog")
    .getByRole("button", { name: "Revoke", exact: true })
    .click();
  await expect(
    page.getByRole("cell", { name: "Revoked", exact: true }),
  ).toBeVisible();
  await page.getByRole("link", { name: "Overview", exact: true }).click();
  await expect(
    page.getByRole("heading", { name: "A clear view of your AI traffic." }),
  ).toBeVisible();
  await page.getByLabel("Appearance", { exact: true }).selectOption("light");
  await page.screenshot({
    path: "../../output/playwright/overview-light-en.png",
    fullPage: true,
  });
  await page.setViewportSize({ width: 390, height: 844 });
  await expect(page.locator("body")).toHaveJSProperty("scrollWidth", 390);
  await page.screenshot({
    path: "../../output/playwright/overview-mobile.png",
    fullPage: true,
  });
});
