import assert from "node:assert/strict";
import { existsSync, readFileSync } from "node:fs";
import { resolve } from "node:path";

interface PackageJson {
  name: string;
  version: string;
  private: boolean;
  type: string;
  scripts: Record<string, string>;
  dependencies?: Record<string, string>;
  devDependencies?: Record<string, string>;
}

// eslint-disable-next-line @typescript-eslint/no-explicit-any
const readJson = (path: string): any => JSON.parse(readFileSync(path, "utf8"));
const packageJson = readJson("package.json") as PackageJson;

assert.equal(
  packageJson.name,
  "prompt-shield-frontend",
  "package name must be prompt-shield-frontend",
);
assert.equal(packageJson.private, true, "package must be private");
assert.equal(packageJson.type, "module", "package type must be module");
assert.ok(packageJson.version, "package must have a version");
assert.ok(
  packageJson.scripts?.build,
  "package must define a production build script",
);
assert.ok(packageJson.scripts?.test, "package must define a test script");
assert.ok(
  packageJson.scripts?.typecheck,
  "package must define a typecheck script",
);

assert.ok(existsSync("index.html"), "index.html must exist at app root");

if (process.argv.includes("--dist")) {
  const distDir = resolve("dist");
  assert.ok(existsSync(distDir), "dist directory must exist after build");
  assert.ok(
    existsSync(resolve(distDir, "index.html")),
    "dist/index.html must exist",
  );
}

console.log(
  `package-metadata ok: name=${packageJson.name} version=${
    packageJson.version
  } packaged=${process.argv.includes("--dist")}`,
);
