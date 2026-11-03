import assert from "node:assert/strict";
import {
  mkdtemp,
  mkdir,
  readFile,
  writeFile,
  readdir,
  rm,
} from "node:fs/promises";
import { tmpdir } from "node:os";
import { join, resolve } from "node:path";
import { fileURLToPath } from "node:url";
import { test } from "node:test";
import {
  verifyConsumer,
  packageName,
} from "@qqdevelopments/prompt-shield-web-core/distribution";
import { synchronize } from "./update-shared.ts";

const app = resolve(fileURLToPath(new URL("..", import.meta.url)));
const realMetadata = JSON.parse(
  await readFile(join(app, "vendor/shared-core.json"), "utf8"),
);
const archive = await readFile(join(app, "vendor", realMetadata.filename));
const initialVersion = realMetadata.version as string;
const versionParts = initialVersion.split(".").map(Number);
const nextVersion = `${versionParts[0]}.${versionParts[1]}.${versionParts[2] + 1}`;
async function fixture() {
  const root = await mkdtemp(join(tmpdir(), "shield-shared-update-test-"));
  const community = join(root, "community");
  const enterprise = join(root, "enterprise");
  const targets = [
    join(community, "apps/web-console"),
    join(enterprise, "apps/web-dashboard"),
  ];
  const source = join(community, "packages/web-core");
  await mkdir(source, { recursive: true });
  await writeFile(
    join(source, "package.json"),
    JSON.stringify({ name: packageName, version: initialVersion }),
  );
  await writeFile(
    join(source, "package-lock.json"),
    JSON.stringify({
      version: initialVersion,
      packages: { "": { version: initialVersion } },
    }),
  );
  for (const [i, target] of targets.entries()) {
    await mkdir(target, { recursive: true });
    await writeFile(
      join(target, "package.json"),
      JSON.stringify({
        name:
          i === 0
            ? "prompt-shield-frontend"
            : "prompt-shield-enterprise-frontend",
        dependencies: {},
      }),
    );
    await writeFile(
      join(target, "package-lock.json"),
      JSON.stringify({
        lockfileVersion: 3,
        packages: { "": { dependencies: {} } },
      }),
    );
  }
  const prepare = async () => ({
    data: archive,
    verify: verifyConsumer,
    dispose: async () => {},
  });
  const updateLock = async (target: string) => {
    const manifest = JSON.parse(
      await readFile(join(target, "package.json"), "utf8"),
    );
    const meta = JSON.parse(
      await readFile(join(target, "vendor/shared-core.json"), "utf8"),
    );
    await writeFile(
      join(target, "package-lock.json"),
      JSON.stringify({
        lockfileVersion: 3,
        packages: {
          "": { dependencies: manifest.dependencies },
          ["node_modules/" + packageName]: {
            version: meta.version,
            resolved: manifest.dependencies[packageName],
            integrity: meta.integrity,
          },
        },
      }),
    );
  };
  return {
    root,
    community,
    enterprise,
    targets,
    prepare,
    updateLock,
    options: { communityRoot: community, enterpriseRoot: enterprise },
  };
}
test("updates both real consumer boundaries, checks integrity and is idempotent", async () => {
  const f = await fixture();
  try {
    await synchronize(f.options, f);
    for (const target of f.targets)
      assert.equal((await verifyConsumer(target)).version, initialVersion);
    const before = await Promise.all(
      f.targets.map((target) =>
        readFile(join(target, "package-lock.json"), "utf8"),
      ),
    );
    await synchronize(f.options, {
      prepare: f.prepare,
      updateLock: async () => {
        throw new Error("must not rewrite current locks");
      },
    });
    await synchronize({ ...f.options, check: true }, f);
    assert.deepEqual(
      await Promise.all(
        f.targets.map((target) =>
          readFile(join(target, "package-lock.json"), "utf8"),
        ),
      ),
      before,
    );
    await writeFile(
      join(f.targets[1], "vendor", realMetadata.filename),
      "tampered",
    );
    await assert.rejects(
      synchronize({ ...f.options, check: true }, f),
      /Version conflict/,
    );
  } finally {
    await rm(f.root, { recursive: true, force: true });
  }
});
test("rejects invalid target paths before invoking the package producer", async () => {
  const f = await fixture();
  try {
    await assert.rejects(
      synchronize(
        { ...f.options, enterpriseRoot: join(f.root, "absent") },
        {
          prepare: async () => {
            throw new Error("producer should not run");
          },
        },
      ),
      /ENOENT/,
    );
    assert.deepEqual(await readdir(f.targets[0]), [
      "package-lock.json",
      "package.json",
    ]);
  } finally {
    await rm(f.root, { recursive: true, force: true });
  }
});
test("same-version content conflicts cannot replace either consumer", async () => {
  const f = await fixture();
  try {
    await synchronize(f.options, f);
    const before = await readFile(
      join(f.targets[0], "vendor", realMetadata.filename),
    );
    await assert.rejects(
      synchronize(f.options, {
        ...f,
        prepare: async () => ({
          data: Buffer.from("different content"),
          verify: verifyConsumer,
          dispose: async () => {},
        }),
      }),
      /Version conflict/,
    );
    assert.deepEqual(
      await readFile(join(f.targets[0], "vendor", realMetadata.filename)),
      before,
    );
  } finally {
    await rm(f.root, { recursive: true, force: true });
  }
});
test("failed second lock update restores exact pre-existing files in both consumers", async () => {
  const f = await fixture();
  try {
    await synchronize(f.options, f);
    const originals = await Promise.all(
      f.targets.flatMap((target) =>
        ["package.json", "package-lock.json"].map((file) =>
          readFile(join(target, file), "utf8"),
        ),
      ),
    );
    let calls = 0;
    await assert.rejects(
      synchronize(
        { ...f.options, version: nextVersion },
        {
          ...f,
          updateLock: async (target) => {
            await f.updateLock(target);
            if (++calls === 2)
              throw new Error("controlled lock update failure");
          },
        },
      ),
      /controlled lock update failure/,
    );
    assert.deepEqual(
      await Promise.all(
        f.targets.flatMap((target) =>
          ["package.json", "package-lock.json"].map((file) =>
            readFile(join(target, file), "utf8"),
          ),
        ),
      ),
      originals,
    );
    for (const target of f.targets) {
      assert.equal((await verifyConsumer(target)).version, initialVersion);
      assert.deepEqual(
        await readFile(join(target, "vendor", realMetadata.filename)),
        archive,
      );
      assert.deepEqual(await readdir(join(target, "vendor")), [
        realMetadata.filename,
        "shared-core.json",
      ]);
    }
  } finally {
    await rm(f.root, { recursive: true, force: true });
  }
});
test("check mode accepts an isolated Community checkout and rejects stale locks", async () => {
  const f = await fixture();
  try {
    await synchronize(f.options, f);
    await synchronize({ communityRoot: f.community, check: true }, f);
    await writeFile(join(f.targets[0], "package-lock.json"), "{}");
    await assert.rejects(
      synchronize({ ...f.options, check: true }, f),
      /dependency\/lock mismatch/,
    );
  } finally {
    await rm(f.root, { recursive: true, force: true });
  }
});

test("a new version updates source metadata and retains the previous archive", async () => {
  const f = await fixture();
  try {
    await synchronize(f.options, f);
    await synchronize({ ...f.options, version: nextVersion }, f);
    for (const target of f.targets) {
      assert.equal((await verifyConsumer(target)).version, nextVersion);
      assert.deepEqual(
        await readFile(join(target, "vendor", realMetadata.filename)),
        archive,
      );
    }
    const source = join(f.community, "packages/web-core");
    assert.equal(
      JSON.parse(await readFile(join(source, "package.json"), "utf8")).version,
      nextVersion,
    );
    assert.equal(
      JSON.parse(await readFile(join(source, "package-lock.json"), "utf8"))
        .packages[""].version,
      nextVersion,
    );
  } finally {
    await rm(f.root, { recursive: true, force: true });
  }
});
