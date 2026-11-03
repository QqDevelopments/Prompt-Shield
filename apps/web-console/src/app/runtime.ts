export type RuntimeConfig = {
  apiBaseURL: string;
  productMode: "community" | "enterprise";
};
let config: RuntimeConfig = { apiBaseURL: "", productMode: "community" };

export function parseRuntime(value: unknown): RuntimeConfig {
  if (!value || typeof value !== "object")
    throw new Error("invalid_runtime_config");
  const raw = value as Record<string, unknown>;
  if (
    typeof raw.apiBaseURL !== "string" ||
    !["community", "enterprise"].includes(String(raw.productMode))
  )
    throw new Error("invalid_runtime_config");
  if (raw.apiBaseURL !== "") {
    const url = new URL(raw.apiBaseURL);
    const local = ["localhost", "127.0.0.1", "[::1]"].includes(url.hostname);
    if (
      (url.protocol !== "https:" && !(local && url.protocol === "http:")) ||
      url.username ||
      url.password ||
      url.search ||
      url.hash ||
      url.pathname !== "/"
    )
      throw new Error("invalid_runtime_config");
  }
  return {
    apiBaseURL: raw.apiBaseURL.replace(/\/$/, ""),
    productMode: raw.productMode as RuntimeConfig["productMode"],
  };
}
export async function loadRuntime() {
  const response = await fetch("/config.json", {
    cache: "no-store",
    credentials: "omit",
  });
  if (!response.ok) throw new Error("runtime_config_unavailable");
  config = parseRuntime(await response.json());
}
export function apiURL(path: string) {
  return `${config.apiBaseURL}${path}`;
}
export function productMode() {
  return config.productMode;
}
