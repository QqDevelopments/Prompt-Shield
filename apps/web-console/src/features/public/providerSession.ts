import {
  createCredentialStore,
  isRecord,
  type Provider,
} from "@qqdevelopments/prompt-shield-web-core";
export type { Provider } from "@qqdevelopments/prompt-shield-web-core";

export const providerChatPaths = {
  openai: "chat/completions",
  anthropic: "messages",
  gemini: "gemini/chat/completions",
} as const satisfies Record<Provider, string>;

export function createProviderSession(storage: () => Storage, now = Date.now) {
  return createCredentialStore({
    storage,
    now,
    storageKey: "ps.provider-session.v1",
    codec: {
      decode: (value) => (isRecord(value) ? { ...value, scope: "" } : null),
      encode: ({ key, provider, created, used }) => ({
        key,
        provider,
        created,
        used,
      }),
    },
  });
}
export const providerSession = createProviderSession(
  () => window.sessionStorage,
);
