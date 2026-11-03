import { defineConfig } from "@playwright/test";
export default defineConfig({
  testDir: "./e2e",
  fullyParallel: false,
  projects: [
    { name: "chromium", use: { browserName: "chromium" } },
    {
      name: "firefox-smoke",
      testMatch: "**/smoke.spec.ts",
      use: { browserName: "firefox" },
    },
    {
      name: "webkit-smoke",
      testMatch: "**/smoke.spec.ts",
      use: { browserName: "webkit" },
    },
  ],
  workers: 1,
  outputDir: "../../output/playwright/results",
  use: {
    baseURL: process.env.PLAYWRIGHT_EXTERNAL_URL ?? "http://127.0.0.1:18080",
    headless: true,
    trace: "retain-on-failure",
  },
  webServer: process.env.PLAYWRIGHT_EXTERNAL_URL
    ? undefined
    : [
        {
          command: "cargo run --quiet",
          cwd: "../../services/engine",
          port: 15051,
          timeout: 120000,
          reuseExistingServer: false,
          env: {
            NER_ENABLED: "false",
            ENGINE_LISTEN: "127.0.0.1:15051",
            ENGINE_TOKEN: "synthetic-e2e-engine-token-not-a-real-secret",
          },
        },
        {
          command: "go run ./cmd/gateway",
          cwd: "../../services/gateway",
          url: "http://127.0.0.1:18080/healthz",
          timeout: 120000,
          reuseExistingServer: false,
          env: {
            ENVIRONMENT: "development",
            DATABASE_URL: "",
            GATEWAY_LISTEN: "127.0.0.1:18080",
            PUBLIC_ORIGIN: "http://127.0.0.1:18080",
            PUBLIC_ENABLED: "true",
            PUBLIC_HMAC_KEY: "synthetic-public-hmac-key-for-testing-only",
            RUST_ENGINE_ADDR: "127.0.0.1:15051",
            ENGINE_TOKEN: "synthetic-e2e-engine-token-not-a-real-secret",
            OPERATOR_PASSWORD: "synthetic-e2e-operator-password",
            OPENAI_API_KEY: "",
            ANTHROPIC_API_KEY: "",
            GEMINI_API_KEY: "",
          },
        },
      ],
});
