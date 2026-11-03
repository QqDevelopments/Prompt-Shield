import { writeFileSync } from "node:fs";
import { resolve } from "node:path";
import { pathToFileURL } from "node:url";

const productionAPIs = new Set([
  "api.prompt-shield.qqdevelopments.com",
  "api.prompt-shield-enterprise.qqdevelopments.com",
]);

export interface HostedConfig {
  apiBaseURL: string;
  productMode: "community" | "enterprise";
}

export function hostedConfig(
  environment: Record<string, string | undefined> = process.env,
): HostedConfig {
  let url: URL;
  try {
    url = new URL(environment.API_BASE_URL ?? "");
  } catch {
    throw new Error(
      "Set API_BASE_URL to the backend HTTPS origin before building.",
    );
  }
  if (
    url.protocol !== "https:" ||
    url.username ||
    url.password ||
    url.search ||
    url.hash ||
    url.pathname !== "/" ||
    ["localhost", "127.0.0.1", "[::1]"].includes(url.hostname)
  ) {
    throw new Error(
      "API_BASE_URL must be a remote HTTPS origin without credentials, path or query.",
    );
  }
  if (
    environment.VERCEL_ENV &&
    environment.VERCEL_ENV !== "production" &&
    productionAPIs.has(url.hostname)
  ) {
    throw new Error(
      "Preview/development builds must use an isolated staging API.",
    );
  }
  return { apiBaseURL: url.origin, productMode: "community" };
}

export function writeHostedConfig(
  directory: string,
  environment: Record<string, string | undefined> = process.env,
): void {
  const config = hostedConfig(environment);
  writeFileSync(
    resolve(directory, "config.json"),
    JSON.stringify(config) + "\n",
  );
}

if (
  process.argv[1] &&
  import.meta.url === pathToFileURL(resolve(process.argv[1])).href
) {
  try {
    if (process.argv.includes("--check")) hostedConfig();
    else writeHostedConfig("dist");
  } catch (error) {
    console.error((error as Error).message);
    process.exitCode = 1;
  }
}
