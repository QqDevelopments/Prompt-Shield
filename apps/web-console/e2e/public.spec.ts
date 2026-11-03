import { expect, test } from "@playwright/test";
test.beforeEach(async ({ page }) => {
  await page.route("**/public/v1/models", (route) =>
    route.fulfill({
      json: { data: [{ id: "gpt-fixture-luna", name: "Fixture Luna" }] },
    }),
  );
});
test("public inspection and session key reload lifecycle", async ({ page }) => {
  await page.goto("/");
  await page.getByLabel("Language", { exact: true }).selectOption("en");
  await page
    .getByLabel("Provider API key", { exact: true })
    .fill("synthetic-browser-provider-key");
  await page.getByRole("button", { name: "Use key for this session" }).click();
  await page.reload();
  await expect(
    page.getByText("Provider key ready for this session", { exact: true }),
  ).toBeVisible();
  await page
    .getByLabel("Your prompt", { exact: true })
    .fill("Email alice@example.com");
  await page.getByRole("button", { name: "Inspect text", exact: true }).click();
  await expect(page.locator(".result pre")).not.toContainText(
    "alice@example.com",
  );
  await expect(page.locator(".result pre")).toContainText("EMAIL");
  await page.screenshot({
    path: "../../output/playwright/public-community.png",
    fullPage: true,
  });
  await page.getByRole("button", { name: "Forget key" }).click();
  await page.reload();
  await expect(
    page.getByLabel("Provider API key", { exact: true }),
  ).toHaveValue("");
});
test("blocked session storage supports memory credentials only", async ({
  page,
}) => {
  await page.addInitScript(() =>
    Object.defineProperty(window, "sessionStorage", {
      get() {
        throw new DOMException("blocked", "SecurityError");
      },
    }),
  );
  await page.goto("/");
  await page.getByLabel("Language", { exact: true }).selectOption("en");
  await page
    .getByLabel("Provider API key", { exact: true })
    .fill("synthetic-memory-key");
  await page.getByRole("button", { name: "Use key for this session" }).click();
  await expect(
    page.getByText("Session storage is unavailable.", { exact: false }),
  ).toBeVisible();
  await page.reload();
  await expect(
    page.getByLabel("Provider API key", { exact: true }),
  ).toHaveValue("");
});

test("inspection renders measured metadata and clears a stale success after failure", async ({
  page,
}) => {
  await page.goto("/");
  await page.getByLabel("Language", { exact: true }).selectOption("en");
  await page
    .getByLabel("Your prompt", { exact: true })
    .fill("Email alice@example.com");
  await page.getByRole("button", { name: "Inspect text", exact: true }).click();
  await expect(page.locator(".result pre")).toContainText("EMAIL");
  await expect(page.locator(".result .entity-tags")).toContainText("Email");
  await expect(page.locator(".result .result-metrics")).toContainText("ms");
  await page.route("**/public/v1/inspect", (route) =>
    route.fulfill({
      status: 503,
      json: { error: { code: "inspection_unavailable" } },
    }),
  );
  await page.getByRole("button", { name: "Inspect text", exact: true }).click();
  await expect(page.getByRole("alert")).toBeVisible();
  await expect(page.locator(".result")).toHaveCount(0);
  await page.unroute("**/public/v1/inspect");
  await page.getByRole("button", { name: "Inspect text", exact: true }).click();
  await expect(page.locator(".result pre")).toContainText("EMAIL");
  await page.getByLabel("Your prompt", { exact: true }).fill("A new draft");
  await expect(page.locator(".result")).toHaveCount(0);
});
