import { beforeEach, describe, expect, it } from "vitest";
import { createProviderSession, type Provider } from "./providerSession";

describe("provider session lifecycle", () => {
  beforeEach(() => sessionStorage.clear());
  it("reads and preserves the existing Community storage format", () => {
    sessionStorage.setItem(
      "ps.provider-session.v1",
      JSON.stringify({
        key: "legacy-key",
        provider: "gemini",
        created: 100,
        used: 100,
      }),
    );
    const session = createProviderSession(
      () => sessionStorage,
      () => 200,
    );
    expect(session.take("gemini")).toBe("legacy-key");
    expect(
      JSON.parse(sessionStorage.getItem("ps.provider-session.v1")!),
    ).toEqual({
      key: "legacy-key",
      provider: "gemini",
      created: 100,
      used: 200,
    });
  });
  it.each<Provider>(["openai", "anthropic", "gemini"])(
    "survives reload and isolates %s credentials",
    (provider) => {
      const first = createProviderSession(() => sessionStorage);
      first.save(provider, "synthetic-key");
      const reloaded = createProviderSession(() => sessionStorage);
      expect(reloaded.take(provider)).toBe("synthetic-key");
      for (const other of ["openai", "anthropic", "gemini"] as const) {
        if (other !== provider)
          expect(() => reloaded.take(other)).toThrow("provider_key_required");
      }
      reloaded.forget();
      expect(
        createProviderSession(() => sessionStorage).status().available,
      ).toBe(false);
    },
  );
  it("expires on idle, absolute age, and restored expired browser sessions", () => {
    let now = 1000;
    const session = createProviderSession(
      () => sessionStorage,
      () => now,
    );
    session.save("openai", "synthetic-key");
    now += 30 * 60 * 1000;
    expect(() => session.take("openai")).toThrow("provider_key_required");
    session.save("openai", "synthetic-key");
    for (let i = 0; i < 47; i++) {
      now += 10 * 60 * 1000;
      session.take("openai");
    }
    now += 10 * 60 * 1000;
    expect(
      createProviderSession(
        () => sessionStorage,
        () => now,
      ).status().available,
    ).toBe(false);
  });
  it("falls back to memory when storage access or writes fail", () => {
    const blocked = () => {
      throw new DOMException("blocked", "SecurityError");
    };
    const session = createProviderSession(blocked);
    session.save("anthropic", "synthetic-key");
    expect(session.status().memoryOnly).toBe(true);
    expect(session.take("anthropic")).toBe("synthetic-key");
    expect(createProviderSession(blocked).status().available).toBe(false);
    session.forget();
    expect(() => session.take("anthropic")).toThrow();
  });
});
