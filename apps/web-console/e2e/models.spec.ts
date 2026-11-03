import { expect, test, type Page } from "@playwright/test";

test("Gemini discovery requires explicit model selection and uses its own chat route", async ({
  page,
}) => {
  await page.route("**/public/v1/models", async (route) => {
    expect(route.request().postDataJSON().provider).toBe("gemini");
    expect(route.request().headers()["x-provider-api-key"]).toBe(
      "synthetic-gemini-browser",
    );
    await route.fulfill({
      json: { data: [{ id: "gemini-fixture", name: "Gemini fixture" }] },
    });
  });
  await page.route("**/public/v1/gemini/chat/completions", async (route) => {
    expect(route.request().postDataJSON().model).toBe("gemini-fixture");
    expect(route.request().headers()["x-provider-api-key"]).toBe(
      "synthetic-gemini-browser",
    );
    await route.fulfill({
      json: {
        choices: [
          { message: { role: "assistant", content: "Synthetic Gemini reply" } },
        ],
      },
    });
  });
  await page.goto("/en");
  await page.getByLabel("Provider", { exact: true }).selectOption("gemini");
  await page
    .getByLabel("Provider API key", { exact: true })
    .fill("synthetic-gemini-browser");
  await page.getByRole("button", { name: "Use key for this session" }).click();
  await expect(
    page.getByRole("option", { name: "Gemini fixture (gemini-fixture)" }),
  ).toBeAttached();
  await expect(page.getByLabel("Model", { exact: true })).toHaveValue("");
  await page
    .getByLabel("Model", { exact: true })
    .selectOption("gemini-fixture");
  await page
    .getByLabel("Your prompt", { exact: true })
    .fill("Synthetic greeting");
  await page.getByRole("button", { name: "Send protected message" }).click();
  await expect(
    page.getByText("Synthetic Gemini reply", { exact: true }),
  ).toBeVisible();
  await page.reload();
  await expect(
    page.getByText("Provider key ready for this session", { exact: true }),
  ).toBeVisible();
  await expect(page.getByLabel("Provider", { exact: true })).toHaveValue(
    "gemini",
  );
  await page.getByLabel("Provider", { exact: true }).selectOption("openai");
  await expect(
    page.getByLabel("Provider API key", { exact: true }),
  ).toHaveValue("");
  await expect(
    page.getByText("Synthetic Gemini reply", { exact: true }),
  ).toHaveCount(0);
});

async function enterKey(page: Page) {
  await page
    .getByLabel("Provider API key", { exact: true })
    .fill("synthetic-model-key");
  await page.getByRole("button", { name: "Use key for this session" }).click();
}

test.beforeEach(async ({ page }) => {
  await page.route("**/api/session", (route) =>
    route.fulfill({ status: 401, json: { error: { code: "unauthorized" } } }),
  );
  await page.goto("/");
  await page.getByLabel("Language", { exact: true }).selectOption("en");
});

test("account models select Luna and Sonnet, refresh, switch, reload and forget", async ({
  page,
}) => {
  let calls = 0;
  await page.route("**/public/v1/models", async (route) => {
    calls++;
    expect(route.request().headers()["x-provider-api-key"]).toBe(
      "synthetic-model-key",
    );
    const { provider } = route.request().postDataJSON();
    await route.fulfill({
      json: {
        data:
          provider === "openai"
            ? [
                { id: "gpt-fixture-astra", name: "Astra" },
                { id: "gpt-new-luna", name: "Luna" },
                { id: "gpt-old-luna", name: "Older Luna" },
              ]
            : [
                { id: "claude-fixture-opus", name: "Opus" },
                { id: "claude-new-sonnet", name: "Sonnet" },
                { id: "claude-old-sonnet", name: "Older Sonnet" },
              ],
      },
    });
  });
  await expect(
    page.getByText("Enter your provider key to load available models."),
  ).toBeVisible();
  await enterKey(page);
  await expect(page.getByLabel("Model", { exact: true })).toHaveValue(
    "gpt-new-luna",
  );
  await page
    .getByLabel("Model", { exact: true })
    .selectOption("gpt-fixture-astra");
  await page.getByRole("button", { name: "Refresh models" }).click();
  await expect(page.getByLabel("Model", { exact: true })).toHaveValue(
    "gpt-new-luna",
  );
  await page.reload();
  await expect(page.getByLabel("Model", { exact: true })).toHaveValue(
    "gpt-new-luna",
  );
  await page.getByLabel("Provider", { exact: true }).selectOption("anthropic");
  await expect(
    page.getByRole("option", { name: "Luna (gpt-new-luna)", exact: true }),
  ).toHaveCount(0);
  await enterKey(page);
  await expect(page.getByLabel("Model", { exact: true })).toHaveValue(
    "claude-new-sonnet",
  );
  await page.route("**/public/v1/messages", async (route) => {
    expect(route.request().postDataJSON().model).toBe("claude-new-sonnet");
    await route.fulfill({
      json: { content: [{ type: "text", text: "Fixture response" }] },
    });
  });
  await page.getByLabel("Your prompt", { exact: true }).fill("Synthetic hello");
  await page.getByRole("button", { name: "Send protected message" }).click();
  await expect(
    page.getByText("Fixture response", { exact: true }),
  ).toBeVisible();
  await page.getByRole("button", { name: "Forget key" }).click();
  await expect(page.getByLabel("Model", { exact: true })).toHaveCount(0);
  expect(calls).toBe(4);
});

test("missing preferred model requires choice; forbidden, empty and invalid-key results are distinct", async ({
  page,
}) => {
  let result: "other" | "forbidden" | "empty" | "invalid" = "other";
  await page.route("**/public/v1/models", (route) => {
    if (result === "forbidden")
      return route.fulfill({
        status: 403,
        json: { error: { code: "models_forbidden" } },
      });
    if (result === "invalid")
      return route.fulfill({
        status: 401,
        json: { error: { code: "provider_key_rejected" } },
      });
    return route.fulfill({
      json: {
        data: result === "empty" ? [] : [{ id: "gpt-other", name: "Other" }],
      },
    });
  });
  await enterKey(page);
  await expect(page.getByLabel("Model", { exact: true })).toBeEnabled();
  await expect(page.getByLabel("Model", { exact: true })).toHaveValue("");
  await page.getByLabel("Your prompt", { exact: true }).fill("Synthetic hello");
  await expect(
    page.getByRole("button", { name: "Send protected message" }),
  ).toBeDisabled();
  await page.getByLabel("Model", { exact: true }).selectOption("gpt-other");
  await expect(
    page.getByRole("button", { name: "Send protected message" }),
  ).toBeEnabled();
  result = "forbidden";
  await page.getByRole("button", { name: "Refresh models" }).click();
  await expect(page.getByRole("alert")).toContainText("cannot list models");
  await expect(
    page.getByText("Provider key ready for this session", { exact: true }),
  ).toBeVisible();
  result = "empty";
  await page.getByRole("button", { name: "Refresh models" }).click();
  await expect(page.getByText("This key returned no models.")).toBeVisible();
  result = "invalid";
  await page.getByRole("button", { name: "Refresh models" }).click();
  await expect(
    page.getByLabel("Provider API key", { exact: true }),
  ).toHaveValue("");
  await expect(page.getByRole("alert")).toContainText(
    "provider rejected this key",
  );
});

test("old backend shows an actionable error and refresh recovers after upgrade", async ({
  page,
}) => {
  let upgraded = false;
  await page.route("**/public/v1/models", (route) =>
    route.fulfill(
      upgraded
        ? { json: { data: [{ id: "gpt-fixture-luna", name: "Luna" }] } }
        : { status: 404, json: { error: { code: "not_found" } } },
    ),
  );
  await enterKey(page);
  await expect(page.getByRole("alert")).toContainText(
    "Rebuild and restart the gateway",
  );
  await expect(page.getByLabel("Model", { exact: true })).toBeDisabled();
  await expect(
    page.getByText("Provider key ready for this session", { exact: true }),
  ).toBeVisible();
  upgraded = true;
  await page.getByRole("button", { name: "Refresh models" }).click();
  await expect(page.getByLabel("Model", { exact: true })).toBeEnabled();
  await expect(page.getByLabel("Model", { exact: true })).toHaveValue(
    "gpt-fixture-luna",
  );
  await expect(page.getByRole("alert")).toHaveCount(0);
});

test("forgetting a key cancels discovery and a late response cannot restore its models", async ({
  page,
}) => {
  let release!: () => void;
  const gate = new Promise<void>((resolve) => {
    release = resolve;
  });
  await page.route("**/public/v1/models", async (route) => {
    await gate;
    await route.fulfill({
      json: { data: [{ id: "gpt-late-luna", name: "Late Luna" }] },
    });
  });
  const requested = page.waitForRequest("**/public/v1/models");
  await enterKey(page);
  await requested;
  await page.getByRole("button", { name: "Forget key" }).click();
  release();
  await expect(
    page.getByLabel("Provider API key", { exact: true }),
  ).toBeVisible();
  await expect(page.getByLabel("Model", { exact: true })).toHaveCount(0);
  await expect(
    page.getByRole("button", { name: "Send protected message" }),
  ).toBeDisabled();
});
