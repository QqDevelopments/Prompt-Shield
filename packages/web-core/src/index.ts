export const providers = ["openai", "anthropic", "gemini"] as const;

export type RequestProgress =
  | { state: "queued"; position: number }
  | { state: "processing" };

export class ProgressError extends Error {
  code: string;
  status: number;
  constructor(code: string, status = 0) {
    super(code);
    this.code = code;
    this.status = status;
  }
}

function progressFailure(value: unknown, status: number): ProgressError {
  const error = isRecord(value) && isRecord(value.error) ? value.error : {};
  return new ProgressError(
    typeof error.code === "string" ? error.code : "invalid_response",
    status,
  );
}

/** Reads the opt-in UI stream without polling, reconnecting or replaying requests. */
export async function readProgressResponse<T>(
  response: Response,
  options: {
    signal?: AbortSignal;
    onProgress?: (progress: RequestProgress) => void;
  } = {},
): Promise<T> {
  const aborted = () => {
    if (options.signal?.aborted)
      throw new DOMException("Request cancelled", "AbortError");
  };
  aborted();
  if (!response.headers.get("content-type")?.startsWith("text/event-stream")) {
    const value: unknown = await response.json().catch(() => {
      aborted();
      throw new ProgressError("invalid_response");
    });
    aborted();
    if (!response.ok) throw progressFailure(value, response.status);
    return value as T;
  }
  if (!response.ok || !response.body)
    throw new ProgressError("invalid_response", response.status);
  const reader = response.body.getReader();
  const decoder = new TextDecoder();
  let pending = "";
  let processing = false;
  const cancel = () => {
    void reader.cancel().catch(() => {});
  };
  options.signal?.addEventListener("abort", cancel, { once: true });
  try {
    for (;;) {
      aborted();
      const { value, done } = await reader.read();
      aborted();
      pending = (pending + decoder.decode(value, { stream: !done })).replace(
        /\r\n/g,
        "\n",
      );
      if (pending.length > 3 * 1024 * 1024)
        throw new ProgressError("invalid_response");
      let end: number;
      while ((end = pending.indexOf("\n\n")) !== -1) {
        const frame = pending.slice(0, end);
        pending = pending.slice(end + 2);
        let event = "";
        const data: string[] = [];
        for (const line of frame.split("\n")) {
          if (line.startsWith("event:")) event = line.slice(6).trim();
          if (line.startsWith("data:")) data.push(line.slice(5).trimStart());
        }
        if (!data.length) continue;
        let body: unknown;
        try {
          body = JSON.parse(data.join("\n"));
        } catch {
          throw new ProgressError("invalid_response");
        }
        aborted();
        if (event === "queued") {
          if (
            processing ||
            !isRecord(body) ||
            !Number.isSafeInteger(body.position) ||
            (body.position as number) < 1
          )
            throw new ProgressError("invalid_response");
          options.onProgress?.({
            state: "queued",
            position: body.position as number,
          });
        } else if (event === "processing") {
          if (processing) throw new ProgressError("invalid_response");
          processing = true;
          options.onProgress?.({ state: "processing" });
        } else if (event === "result") {
          if (!processing) throw new ProgressError("invalid_response");
          return body as T;
        } else if (event === "error") {
          if (!isRecord(body)) throw new ProgressError("invalid_response");
          throw progressFailure(
            body.response ?? body,
            typeof body.http_status === "number" ? body.http_status : 503,
          );
        } else throw new ProgressError("invalid_response");
      }
      if (done) throw new ProgressError("invalid_response");
    }
  } finally {
    options.signal?.removeEventListener("abort", cancel);
    await reader.cancel().catch(() => {});
    reader.releaseLock();
  }
}
export type Provider = (typeof providers)[number];
export function isProvider(value: unknown): value is Provider {
  return providers.some((provider) => provider === value);
}

export type Locale = "en" | "tr";
export type PublicLocale = Locale;
export type Theme = "light" | "dark" | "system";

export function resolveLocale(
  saved: string | null,
  languages: readonly string[],
): Locale {
  if (saved === "en" || saved === "tr") return saved;
  for (const language of languages) {
    const base = language.toLowerCase().split("-")[0];
    if (base === "en" || base === "tr") return base;
  }
  return "en";
}
export function resolveTheme(saved: string | null): Theme {
  return saved === "light" || saved === "dark" ? saved : "system";
}
export function publicEntryLocale(pathname: string): PublicLocale | null {
  const path = pathname.replace(/\/$/, "");
  return path === "/en" ? "en" : path === "/tr" ? "tr" : null;
}
export function isPublicEntry(pathname: string): boolean {
  return pathname === "/" || publicEntryLocale(pathname) !== null;
}
export function publicEntryPath(
  locale: PublicLocale,
  search = "",
  hash = "",
): string {
  const query = new URLSearchParams(search);
  query.delete("lang");
  const value = query.toString();
  return "/" + locale + (value ? "?" + value : "") + hash;
}

export type CredentialRecord = {
  key: string;
  provider: Provider;
  scope: string;
  created: number;
  used: number;
};
export type CredentialCodec = {
  decode: (value: unknown) => unknown;
  encode: (value: CredentialRecord) => unknown;
};
export function isRecord(value: unknown): value is Record<string, unknown> {
  return value !== null && typeof value === "object" && !Array.isArray(value);
}
function validCredential(value: unknown): value is CredentialRecord {
  return (
    isRecord(value) &&
    typeof value.key === "string" &&
    /^[\x21-\x7e]{1,4096}$/.test(value.key) &&
    isProvider(value.provider) &&
    typeof value.scope === "string" &&
    typeof value.created === "number" &&
    typeof value.used === "number" &&
    Number.isFinite(value.created) &&
    Number.isFinite(value.used) &&
    value.created <= value.used
  );
}

export function createCredentialStore(options: {
  storage: () => Storage;
  storageKey: string;
  codec: CredentialCodec;
  now?: () => number;
}) {
  const now = options.now ?? (() => Date.now());
  let credential: CredentialRecord | null = null;
  let memoryOnly = false;
  const persist = () => {
    try {
      if (credential)
        options
          .storage()
          .setItem(
            options.storageKey,
            JSON.stringify(options.codec.encode(credential)),
          );
      else options.storage().removeItem(options.storageKey);
    } catch {
      memoryOnly = true;
    }
  };
  const forget = () => {
    credential = null;
    persist();
  };
  const current = () => {
    if (
      credential &&
      (now() < credential.used ||
        now() - credential.created >= 8 * 60 * 60 * 1000 ||
        now() - credential.used >= 30 * 60 * 1000)
    )
      forget();
    return credential;
  };
  try {
    const raw: unknown = JSON.parse(
      options.storage().getItem(options.storageKey) ?? "null",
    );
    if (raw !== null) {
      const decoded = options.codec.decode(raw);
      if (validCredential(decoded)) credential = decoded;
      else forget();
    }
  } catch {
    memoryOnly = true;
  }
  current();
  return {
    save(provider: Provider, key: string, scope = "") {
      const value = { key, provider, scope, created: now(), used: now() };
      if (!validCredential(value)) throw new Error("invalid_provider_key");
      credential = value;
      persist();
    },
    take(provider: Provider, scope = "") {
      const value = current();
      if (!value || value.provider !== provider || value.scope !== scope)
        throw new Error("provider_key_required");
      value.used = now();
      persist();
      return value.key;
    },
    status(provider?: Provider, scope = "") {
      const value = current();
      return {
        provider: value?.provider,
        available:
          !!value &&
          value.scope === scope &&
          (provider === undefined || value.provider === provider),
        memoryOnly,
      };
    },
    forget,
  };
}
