import { createHash } from "node:crypto";
import { readFile, mkdir, writeFile, rename } from "node:fs/promises";
import { fileURLToPath } from "node:url";
import path from "node:path";

interface ModelFile {
  local: string;
  remote: string;
  sha256: string;
}

interface ModelManifest {
  model: string;
  revision: string;
  files: ModelFile[];
}

const root = path.resolve(path.dirname(fileURLToPath(import.meta.url)), "..");
const manifestContent = await readFile(
  path.join(root, "models/general-ner.json"),
  "utf8",
);
const manifest = JSON.parse(manifestContent) as ModelManifest;
const output = path.join(root, "models/general-ner");
await mkdir(output, { recursive: true });

for (const file of manifest.files) {
  const destination = path.join(output, file.local);
  const digest = (bytes: Buffer | Uint8Array): string =>
    createHash("sha256").update(bytes).digest("hex");

  try {
    const existing = await readFile(destination);
    if (digest(existing) === file.sha256) {
      console.log(`${file.local}: verified existing artifact`);
      continue;
    }
  } catch {
    /* Download missing artifacts. */
  }

  const url = `https://huggingface.co/${manifest.model}/resolve/${manifest.revision}/${file.remote}`;
  const response = await fetch(url);
  if (!response.ok) {
    throw new Error(`Model download failed: ${response.status}`);
  }

  const bytes = Buffer.from(await response.arrayBuffer());
  if (digest(bytes) !== file.sha256) {
    throw new Error(`Checksum mismatch: ${file.local}`);
  }

  await writeFile(`${destination}.part`, bytes);
  await rename(`${destination}.part`, destination);
  console.log(
    `${file.local}: downloaded and SHA-256 verified (${bytes.length} bytes)`,
  );
}
