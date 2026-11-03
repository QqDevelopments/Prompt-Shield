import assert from "node:assert/strict";
import { test } from "node:test";
import {
  createCredentialStore,
  isProvider,
  providers,
  resolveLocale,
  resolveTheme,
  publicEntryLocale,
  publicEntryPath,
  isPublicEntry,
  type CredentialCodec,
  readProgressResponse,
  ProgressError,
} from "../src/index.ts";

test("progress stream handles split UTF-8, comments and real transitions", async () => {
  const raw = new TextEncoder().encode(
    ': heartbeat\n\nevent: queued\ndata: {"position":2}\n\nevent: processing\ndata: {}\n\nevent: result\ndata: {"text":"İstanbul ✓"}\n\n',
  );
  const stream = new ReadableStream<Uint8Array>({
    start(controller) {
      for (const byte of raw) controller.enqueue(Uint8Array.of(byte));
      controller.close();
    },
  });
  const states: unknown[] = [];
  const result = await readProgressResponse(
    new Response(stream, { headers: { "Content-Type": "text/event-stream" } }),
    { onProgress: (state) => states.push(state) },
  );
  assert.deepEqual(result, { text: "İstanbul ✓" });
  assert.deepEqual(states, [
    { state: "queued", position: 2 },
    { state: "processing" },
  ]);
});

test("progress errors preserve safe status, truncated streams fail, JSON stays compatible", async () => {
  const result = await readProgressResponse(
    new Response('{"ok":true}', {
      headers: { "Content-Type": "application/json" },
    }),
  );
  assert.deepEqual(result, { ok: true });
  for (const raw of [
    "event: processing\ndata: {}\n\n",
    'event: queued\ndata: {"position":0}\n\n',
  ]) {
    await assert.rejects(
      readProgressResponse(
        new Response(raw, { headers: { "Content-Type": "text/event-stream" } }),
      ),
      ProgressError,
    );
  }
  await assert.rejects(
    readProgressResponse(
      new Response(
        'event: error\ndata: {"http_status":503,"response":{"error":{"code":"queue_timeout"}}}\n\n',
        { headers: { "Content-Type": "text/event-stream" } },
      ),
    ),
    (error: unknown) =>
      error instanceof ProgressError &&
      error.code === "queue_timeout" &&
      error.status === 503,
  );
});

test("progress accepts CRLF split between chunks and bounds incomplete frames", async () => {
  const raw = new TextEncoder().encode(
    'event: processing\r\ndata: {}\r\n\r\nevent: result\r\ndata: {"ok":true}\r\n\r\n',
  );
  const stream = new ReadableStream<Uint8Array>({
    start(controller) {
      for (const byte of raw) controller.enqueue(Uint8Array.of(byte));
      controller.close();
    },
  });
  assert.deepEqual(
    await readProgressResponse(
      new Response(stream, {
        headers: { "Content-Type": "text/event-stream" },
      }),
    ),
    { ok: true },
  );
  await assert.rejects(
    readProgressResponse(
      new Response("data: " + "x".repeat(3 * 1024 * 1024), {
        headers: { "Content-Type": "text/event-stream" },
      }),
    ),
    ProgressError,
  );
});

test("aborting a queued stream prevents late progress and results", async () => {
  const controller = new AbortController();
  const raw =
    'event: queued\ndata: {"position":1}\n\nevent: processing\ndata: {}\n\nevent: result\ndata: {"late":true}\n\n';
  const states: unknown[] = [];
  await assert.rejects(
    readProgressResponse(
      new Response(raw, { headers: { "Content-Type": "text/event-stream" } }),
      {
        signal: controller.signal,
        onProgress: (state) => {
          states.push(state);
          controller.abort();
        },
      },
    ),
    (error: unknown) =>
      error instanceof DOMException && error.name === "AbortError",
  );
  assert.deepEqual(states, [{ state: "queued", position: 1 }]);
});

class MemoryStorage implements Storage {
  values = new Map<string, string>();
  get length() {
    return this.values.size;
  }
  clear() {
    this.values.clear();
  }
  getItem(key: string) {
    return this.values.get(key) ?? null;
  }
  setItem(key: string, value: string) {
    this.values.set(key, value);
  }
  removeItem(key: string) {
    this.values.delete(key);
  }
  key(index: number) {
    return [...this.values.keys()][index] ?? null;
  }
}
const codec: CredentialCodec = {
  decode: (value) => value,
  encode: (value) => value,
};
function fixture(storage = new MemoryStorage()) {
  let time = 1000;
  const create = () =>
    createCredentialStore({
      storage: () => storage,
      storageKey: "fixture",
      codec,
      now: () => time,
    });
  return {
    storage,
    create,
    advance: (value: number) => {
      time += value;
    },
  };
}

test("provider credentials survive reload and stay scoped", () => {
  for (const provider of providers) {
    const f = fixture();
    f.create().save(provider, "synthetic-key", "org-a");
    const next = f.create();
    assert.equal(next.take(provider, "org-a"), "synthetic-key");
    assert.equal(next.status(provider, "org-b").available, false);
    assert.throws(() => next.take(provider, "org-b"), /provider_key_required/);
    for (const other of providers.filter((value) => value !== provider))
      assert.throws(() => next.take(other, "org-a"), /provider_key_required/);
    next.forget();
    assert.equal(f.create().status().available, false);
    assert.equal(f.storage.getItem("fixture"), null);
  }
  assert.equal(isProvider("toString"), false);
});
test("idle, absolute and backwards-clock expiry purge restored credentials", () => {
  const f = fixture();
  const store = f.create();
  store.save("openai", "synthetic-key");
  f.advance(30 * 60 * 1000);
  assert.throws(() => store.take("openai"), /provider_key_required/);
  store.save("openai", "synthetic-key");
  for (let i = 0; i < 47; i++) {
    f.advance(10 * 60 * 1000);
    store.take("openai");
  }
  f.advance(10 * 60 * 1000);
  assert.equal(f.create().status().available, false);
  store.save("gemini", "synthetic-key");
  f.advance(-1);
  assert.equal(f.create().status().available, false);
});
test("invalid persisted values never become available", () => {
  const f = fixture();
  const base = {
    key: "synthetic",
    provider: "openai",
    scope: "",
    created: 1,
    used: 2,
  };
  for (const value of [
    3,
    [],
    {},
    { ...base, key: "" },
    { ...base, key: "with space" },
    { ...base, key: "x".repeat(4097) },
    { ...base, provider: "invalid" },
    { ...base, scope: null },
    { ...base, created: 3 },
    { ...base, used: "2" },
  ]) {
    f.storage.setItem("fixture", JSON.stringify(value));
    assert.equal(f.create().status().available, false);
  }
  assert.throws(
    () => f.create().save("openai", "invalid\nkey"),
    /invalid_provider_key/,
  );
});
test("unavailable reads and failed writes retain only page memory", () => {
  const blocked = () => {
    throw new Error("storage unavailable");
  };
  const first = createCredentialStore({
    storage: blocked,
    storageKey: "fixture",
    codec,
  });
  first.save("anthropic", "synthetic-key");
  assert.equal(first.status().memoryOnly, true);
  assert.equal(first.take("anthropic"), "synthetic-key");
  assert.equal(
    createCredentialStore({
      storage: blocked,
      storageKey: "fixture",
      codec,
    }).status().available,
    false,
  );
  first.forget();
  assert.throws(() => first.take("anthropic"));
  const storage = new MemoryStorage();
  storage.setItem = () => {
    throw new Error("quota");
  };
  const second = createCredentialStore({
    storage: () => storage,
    storageKey: "fixture",
    codec,
  });
  second.save("gemini", "synthetic-key");
  assert.equal(second.status().memoryOnly, true);
  assert.equal(second.take("gemini"), "synthetic-key");
});
test("locale routes preserve unrelated parameters/fragments and remove legacy lang", () => {
  assert.equal(
    publicEntryPath("tr", "?draft=one&lang=en&tag=a&tag=b", "#result"),
    "/tr?draft=one&tag=a&tag=b#result",
  );
  assert.equal(publicEntryLocale("/en/"), "en");
  assert.equal(publicEntryLocale("/workspace/en"), null);
  assert.equal(isPublicEntry("/"), true);
  assert.equal(isPublicEntry("/admin"), false);
});
test("language and appearance normalization keep current defaults", () => {
  assert.equal(resolveLocale("tr", ["en"]), "tr");
  assert.equal(resolveLocale(null, ["de-DE", "TR-tr"]), "tr");
  assert.equal(resolveLocale("invalid", ["de"]), "en");
  assert.equal(resolveTheme("dark"), "dark");
  assert.equal(resolveTheme("light"), "light");
  assert.equal(resolveTheme("invalid"), "system");
});
