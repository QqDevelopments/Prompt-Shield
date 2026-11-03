import assert from "node:assert/strict";

const base = process.env.SMOKE_BASE_URL ?? "http://localhost:8080";
const password = process.env.OPERATOR_PASSWORD;
if (!password) throw new Error("OPERATOR_PASSWORD is required");

let cookie = "";
let csrf = "";

interface CallHeaders {
  "Content-Type": string;
  Origin: string;
  Cookie: string;
  "X-CSRF-Token": string;
}

// eslint-disable-next-line @typescript-eslint/no-explicit-any
async function call(route: string, method = "GET", body?: any): Promise<any> {
  const headers: CallHeaders = {
    "Content-Type": "application/json",
    Origin: base,
    Cookie: cookie,
    "X-CSRF-Token": csrf,
  };
  const response = await fetch(base + route, {
    method,
    headers,
    body: body === undefined ? undefined : JSON.stringify(body),
  });
  if (response.headers.has("set-cookie")) {
    const setCookie = response.headers.get("set-cookie");
    if (setCookie) {
      cookie = setCookie.split(";")[0];
    }
  }
  const data = await response.json();
  assert.equal(
    response.status,
    route === "/api/keys" && method === "POST" ? 201 : 200,
    `Unexpected status at ${route}: ${response.status}`,
  );
  return data;
}

await call("/readyz");
csrf = (await call("/api/login", "POST", { password })).csrf_token;
const original = await call("/api/policy");
let key: { key: { id: string }; secret: string } | undefined;

try {
  const overview = await call("/api/overview");
  const masked = await call("/api/inspect", "POST", {
    text: "Email alice@example.com about 4111 1111 1111 1111.",
  });
  assert.ok(masked.entity_count >= 2);
  assert.ok(!masked.sanitized_text.includes("alice@example.com"));

  const blocked = await call("/api/inspect", "POST", {
    text: "sk-abcdefghijklmnopqrstuvwx",
  });
  assert.equal(blocked.is_blocked, true);

  if (overview.contextual_ready) {
    await call("/api/policy", "PUT", { ...original, contextual_pii: true });
    const ner = await call("/api/inspect", "POST", {
      text: "John Smith works at Microsoft in London.",
    });
    assert.ok(ner.entity_types.includes("PERSON"));
    assert.ok(ner.entity_types.includes("ORGANIZATION"));
  }

  key = await call("/api/keys", "POST", {
    name: "Synthetic smoke verification",
  });
  const listed = await call("/api/keys");
  assert.ok(key && !JSON.stringify(listed).includes(key.secret));

  const events = await call("/api/events");
  assert.ok(!JSON.stringify(events).includes("alice@example.com"));
  assert.ok(!JSON.stringify(events).includes("John Smith"));

  console.log(
    JSON.stringify({
      ready: true,
      rules: "passed",
      ner: overview.contextual_ready ? "passed" : "not enabled",
      audit_privacy: "passed",
      keys: "passed",
    }),
  );
} finally {
  if (key) await call(`/api/keys/${key.key.id}`, "DELETE");
  const current = await call("/api/policy");
  await call("/api/policy", "PUT", { ...original, version: current.version });
  await call("/api/logout", "POST");
}
