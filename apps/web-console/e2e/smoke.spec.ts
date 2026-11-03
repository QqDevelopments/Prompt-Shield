import { expect, test } from "@playwright/test";

test("public layout, keyboard entry and persisted appearance across desktop and mobile", async ({
  page,
  browserName,
}, testInfo) => {
  await page.goto("/");
  await page.getByLabel("Language", { exact: true }).selectOption("en");
  for (const width of [1440, 390]) {
    await page.setViewportSize({ width, height: 900 });
    await expect(page.getByLabel("Your prompt", { exact: true })).toBeVisible();
    await expect(
      page.getByRole("button", { name: "Inspect text", exact: true }),
    ).toBeVisible();
    expect(
      await page.evaluate(
        () => document.documentElement.scrollWidth <= window.innerWidth,
      ),
    ).toBe(true);
    await page.getByLabel("Appearance", { exact: true }).selectOption("dark");
    await expect(page.locator("html")).toHaveAttribute("data-theme", "dark");
    await page.screenshot({
      path: testInfo.outputPath(`entry-${width}-dark.png`),
      fullPage: true,
      animations: "disabled",
    });
    await page.reload();
    await expect(page.locator("html")).toHaveAttribute("data-theme", "dark");
    await page.getByLabel("Appearance", { exact: true }).selectOption("system");
    await page.emulateMedia({ colorScheme: "light" });
    await expect(page.locator("html")).toHaveAttribute("data-theme", "light");
    await page.screenshot({
      path: testInfo.outputPath(`entry-${width}-light.png`),
      fullPage: true,
      animations: "disabled",
    });
    await page.emulateMedia({ colorScheme: "dark" });
    await expect(page.locator("html")).toHaveAttribute("data-theme", "dark");
  }
  await page.goto("/");
  await expect(page.getByLabel("Your prompt", { exact: true })).toBeVisible();
  await page.keyboard.press("Tab");
  if (browserName === "webkit") {
    // The Windows WebKit fixture tabs through form controls; verify link activation separately.
    await expect(page.getByLabel("Language", { exact: true })).toBeFocused();
    await page.locator(".skip-link").focus();
  }
  await expect(page.locator(".skip-link")).toBeFocused();
  await page.keyboard.press("Enter");
  await expect(page.locator("#public-main")).toBeFocused();
});
