import { createHash } from "node:crypto";
import { readFile } from "node:fs/promises";
import { resolve } from "node:path";

export const packageName = "@qqdevelopments/prompt-shield-web-core";
export type Distribution = {
  name: string;
  version: string;
  filename: string;
  integrity: string;
};
export function integrity(data: Uint8Array): string {
  return "sha512-" + createHash("sha512").update(data).digest("base64");
}
export async function verifyConsumer(root: string): Promise<Distribution> {
  const metadata: Distribution = JSON.parse(
    await readFile(resolve(root, "vendor/shared-core.json"), "utf8"),
  );
  if (
    metadata.name !== packageName ||
    !/^\d+\.\d+\.\d+$/.test(metadata.version) ||
    metadata.filename !==
      "qqdevelopments-prompt-shield-web-core-" + metadata.version + ".tgz"
  )
    throw new Error("Invalid shared package metadata");
  const archive = await readFile(resolve(root, "vendor", metadata.filename));
  if (integrity(archive) !== metadata.integrity)
    throw new Error("Shared archive integrity mismatch");
  const manifest = JSON.parse(
    await readFile(resolve(root, "package.json"), "utf8"),
  );
  const lock = JSON.parse(
    await readFile(resolve(root, "package-lock.json"), "utf8"),
  );
  const specifier = "file:vendor/" + metadata.filename;
  const entry = lock.packages?.["node_modules/" + packageName];
  if (
    manifest.dependencies?.[packageName] !== specifier ||
    lock.packages?.[""]?.dependencies?.[packageName] !== specifier ||
    entry?.version !== metadata.version ||
    entry?.integrity !== metadata.integrity ||
    entry?.resolved !== specifier
  )
    throw new Error("Shared package dependency/lock mismatch");
  return metadata;
}
