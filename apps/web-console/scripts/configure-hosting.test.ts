import assert from "node:assert/strict";
import { mkdtempSync, readFileSync, rmSync } from "node:fs";
import { tmpdir } from "node:os";
import { join } from "node:path";
import test from "node:test";
import { hostedConfig, writeHostedConfig } from "./configure-hosting.ts";

test("writes only public configuration to an independent static artifact", () => {
  const directory = mkdtempSync(join(tmpdir(), "prompt-shield-hosting-"));
  try {
    writeHostedConfig(directory, {
      API_BASE_URL: "https://api.prompt-shield.qqdevelopments.com/",
      VERCEL_ENV: "production",
      OPERATOR_PASSWORD: "synthetic-not-for-the-browser",
    });
    assert.deepEqual(
      JSON.parse(readFileSync(join(directory, "config.json"), "utf8")),
      {
        apiBaseURL: "https://api.prompt-shield.qqdevelopments.com",
        productMode: "community",
      },
    );
  } finally {
    rmSync(directory, { recursive: true, force: true });
  }
});

test("rejects missing, credential-bearing and non-origin deployment addresses", () => {
  for (const API_BASE_URL of [
    undefined,
    "",
    "http://api.example.test",
    "https://localhost",
    "https://key@api.example.test",
    "https://api.example.test/path",
    "https://api.example.test?key=synthetic",
    "https://api.example.test/#fragment",
  ]) {
    assert.throws(() => hostedConfig({ API_BASE_URL }));
  }
});

test("preview builds require a separate API and allow an explicit staging origin", () => {
  for (const VERCEL_ENV of ["preview", "development"]) {
    for (const hostname of [
      "api.prompt-shield.qqdevelopments.com",
      "api.prompt-shield-enterprise.qqdevelopments.com",
    ]) {
      assert.throws(() =>
        hostedConfig({ API_BASE_URL: "https://" + hostname, VERCEL_ENV }),
      );
    }
  }
  assert.equal(
    hostedConfig({
      API_BASE_URL: "https://staging-api.example.test",
      VERCEL_ENV: "preview",
    }).apiBaseURL,
    "https://staging-api.example.test",
  );
});
