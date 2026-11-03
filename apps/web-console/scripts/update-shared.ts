import { execFile } from "node:child_process";
import { createHash } from "node:crypto";
import {
  access,
  mkdtemp,
  readFile,
  realpath,
  rm,
  writeFile,
  mkdir,
} from "node:fs/promises";
import { tmpdir } from "node:os";
import { dirname, resolve, join } from "node:path";
import { fileURLToPath, pathToFileURL } from "node:url";
import { promisify } from "node:util";

const exec = promisify(execFile);
const name = "@qqdevelopments/prompt-shield-web-core";
type Json = Record<string, unknown>;
type Metadata = {
  name: string;
  version: string;
  filename: string;
  integrity: string;
};
type Artifact = {
  data: Buffer;
  verify: (root: string) => Promise<Metadata>;
  dispose: () => Promise<void>;
};
type Options = {
  communityRoot: string;
  enterpriseRoot?: string;
  version?: string;
  check?: boolean;
};
type Dependencies = {
  prepare?: (source: string, version: string) => Promise<Artifact>;
  updateLock?: (root: string) => Promise<void>;
};

async function json(path: string): Promise<Json> {
  return JSON.parse(await readFile(path, "utf8")) as Json;
}
async function exists(path: string) {
  try {
    await access(path);
    return true;
  } catch {
    return false;
  }
}
function digest(data: Buffer) {
  return "sha512-" + createHash("sha512").update(data).digest("base64");
}
async function npm(root: string, args: string[]) {
  const cli = process.env.npm_execpath;
  if (!cli)
    throw new Error(
      "Run the shared helper through npm run shared:update/shared:check",
    );
  await exec(process.execPath, [cli, ...args], {
    cwd: root,
    maxBuffer: 8 * 1024 * 1024,
  });
}
async function prepare(source: string, version: string): Promise<Artifact> {
  const temporary = await mkdtemp(join(tmpdir(), "prompt-shield-web-core-"));
  const dispose = () => rm(temporary, { recursive: true, force: true });
  try {
    await npm(source, ["ci", "--ignore-scripts", "--no-audit", "--no-fund"]);
    await npm(source, ["run", "typecheck"]);
    await npm(source, ["test"]);
    const stage = join(temporary, "package");
    await mkdir(stage);
    await npm(source, ["run", "build", "--", "--outDir", join(stage, "dist")]);
    const manifest = await json(join(source, "package.json"));
    await writeFile(
      join(stage, "package.json"),
      JSON.stringify(
        {
          name,
          version,
          private: true,
          type: "module",
          license: manifest.license,
          exports: manifest.exports,
          files: manifest.files,
        },
        null,
        2,
      ) + "\n",
    );
    await writeFile(
      join(stage, "LICENSE"),
      (await readFile(resolve(source, "../../LICENSE"), "utf8")).replace(
        /\r\n/g,
        "\n",
      ),
    );
    const result = await exec(
      process.execPath,
      [
        process.env.npm_execpath!,
        "pack",
        "--json",
        "--ignore-scripts",
        "--pack-destination",
        temporary,
      ],
      { cwd: stage },
    );
    const packed: { filename: string }[] = JSON.parse(result.stdout);
    const data = await readFile(join(temporary, packed[0].filename));
    const distribution = (await import(
      pathToFileURL(join(stage, "dist/distribution.js")).href
    )) as { verifyConsumer: Artifact["verify"] };
    return { data, verify: distribution.verifyConsumer, dispose };
  } catch (error) {
    await dispose();
    throw error;
  }
}
async function validateFrontend(root: string, expected: string) {
  const actual = await realpath(root);
  if ((await json(join(actual, "package.json"))).name !== expected)
    throw new Error("Unexpected frontend at " + actual);
  await access(join(actual, "package-lock.json"));
  return actual;
}

export async function synchronize(
  options: Options,
  dependencies: Dependencies = {},
) {
  const community = await realpath(options.communityRoot);
  const source = join(community, "packages/web-core");
  const originalManifest = await json(join(source, "package.json"));
  if (originalManifest.name !== name)
    throw new Error("Invalid Community shared package source");
  const version = options.version ?? String(originalManifest.version);
  if (!/^(0|[1-9]\d*)\.(0|[1-9]\d*)\.(0|[1-9]\d*)$/.test(version))
    throw new Error("A stable x.y.z package version is required");
  if (options.check && originalManifest.version !== version)
    throw new Error("Check version must match the source package");
  const targets = [
    await validateFrontend(
      join(community, "apps/web-console"),
      "prompt-shield-frontend",
    ),
  ];
  const enterprise =
    options.enterpriseRoot ?? resolve(community, "../prompt-shield-enterprise");
  if (options.enterpriseRoot || !options.check || (await exists(enterprise))) {
    targets.push(
      await validateFrontend(
        join(enterprise, "apps/web-dashboard"),
        "prompt-shield-enterprise-frontend",
      ),
    );
  }
  if (new Set(targets).size !== targets.length)
    throw new Error("Frontend targets must be distinct");
  const artifact = await (dependencies.prepare ?? prepare)(source, version);
  const metadata: Metadata = {
    name,
    version,
    filename: "qqdevelopments-prompt-shield-web-core-" + version + ".tgz",
    integrity: digest(artifact.data),
  };
  const snapshots = new Map<string, Buffer | null>();
  async function snapshot(path: string) {
    if (!snapshots.has(path))
      snapshots.set(path, (await exists(path)) ? await readFile(path) : null);
  }
  async function write(path: string, value: string | Buffer) {
    await snapshot(path);
    await writeFile(path, value);
  }
  try {
    // Validate every target before changing either repository.
    for (const target of targets) {
      const archive = join(target, "vendor", metadata.filename);
      if (
        (await exists(archive)) &&
        digest(await readFile(archive)) !== metadata.integrity
      )
        throw new Error(
          "Version conflict: choose a new shared package version",
        );
      const record = join(target, "vendor/shared-core.json");
      if (await exists(record)) {
        const old = await json(record);
        if (old.version === version && old.integrity !== metadata.integrity)
          throw new Error(
            "Version conflict: choose a new shared package version",
          );
      }
    }
    if (options.check) {
      for (const target of targets) {
        const current = await artifact.verify(target);
        if (
          current.version !== version ||
          current.integrity !== metadata.integrity
        )
          throw new Error("Shared package is stale: " + target);
      }
      return metadata;
    }
    for (const target of targets) {
      let current: Metadata | undefined;
      try {
        current = await artifact.verify(target);
      } catch {
        /* A missing/stale dependency will be updated. */
      }
      if (
        current?.version === version &&
        current.integrity === metadata.integrity
      )
        continue;
      await mkdir(join(target, "vendor"), { recursive: true });
      await write(join(target, "vendor", metadata.filename), artifact.data);
      await write(
        join(target, "vendor/shared-core.json"),
        JSON.stringify(metadata, null, 2) + "\n",
      );
      const manifestPath = join(target, "package.json");
      const manifest = await json(manifestPath);
      manifest.dependencies = {
        ...((manifest.dependencies as Json) ?? {}),
        [name]: "file:vendor/" + metadata.filename,
      };
      await write(manifestPath, JSON.stringify(manifest, null, 2) + "\n");
      await snapshot(join(target, "package-lock.json"));
      await (
        dependencies.updateLock ??
        ((root) =>
          npm(root, [
            "install",
            "--package-lock-only",
            "--ignore-scripts",
            "--no-audit",
            "--no-fund",
          ]))
      )(target);
      await artifact.verify(target);
    }
    if (originalManifest.version !== version) {
      await write(
        join(source, "package.json"),
        JSON.stringify({ ...originalManifest, version }, null, 2) + "\n",
      );
      const lock = await json(join(source, "package-lock.json"));
      lock.version = version;
      (lock.packages as Record<string, Json>)[""].version = version;
      await write(
        join(source, "package-lock.json"),
        JSON.stringify(lock, null, 2) + "\n",
      );
    }
    return metadata;
  } catch (error) {
    for (const [path, contents] of [...snapshots].reverse()) {
      if (contents === null) await rm(path, { force: true });
      else await writeFile(path, contents);
    }
    throw error;
  } finally {
    await artifact.dispose();
  }
}

async function main() {
  const args = process.argv.slice(2);
  let version: string | undefined;
  let enterpriseRoot: string | undefined;
  let check = false;
  for (let i = 0; i < args.length; i++) {
    if (args[i] === "--check") check = true;
    else if (args[i] === "--version" && args[i + 1]) version = args[++i];
    else if (args[i] === "--enterprise-root" && args[i + 1])
      enterpriseRoot = resolve(args[++i]);
    else throw new Error("Unknown or incomplete shared helper argument");
  }
  const metadata = await synchronize({
    communityRoot: resolve(dirname(fileURLToPath(import.meta.url)), "../../.."),
    version,
    enterpriseRoot,
    check,
  });
  console.log(
    (check ? "Verified" : "Updated") +
      " shared frontend package " +
      metadata.version,
  );
}
if (
  process.argv[1] &&
  import.meta.url === pathToFileURL(resolve(process.argv[1])).href
) {
  main().catch((error: unknown) => {
    console.error(
      error instanceof Error ? error.message : "Shared package update failed",
    );
    process.exitCode = 1;
  });
}
