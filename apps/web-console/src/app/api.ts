import { apiURL } from "./runtime";
import {
  readProgressResponse,
  ProgressError,
  type RequestProgress,
} from "@qqdevelopments/prompt-shield-web-core";
export class APIError extends Error {
  constructor(
    public code: string,
    public status = 0,
  ) {
    super(code);
  }
}
let csrfToken = "";
export function setCSRF(value: string) {
  csrfToken = value;
}
export async function request<T>(
  path: string,
  options: {
    method?: string;
    body?: unknown;
    signal?: AbortSignal;
    onProgress?: (progress: RequestProgress) => void;
  } = {},
): Promise<T> {
  let response: Response;
  try {
    response = await fetch(apiURL(`/api${path}`), {
      method: options.method ?? "GET",
      signal: options.signal,
      credentials: "include",
      headers: {
        "Content-Type": "application/json",
        "X-CSRF-Token": csrfToken,
        ...(options.onProgress ? { "X-Prompt-Shield-Progress": "v1" } : {}),
      },
      body:
        options.body === undefined ? undefined : JSON.stringify(options.body),
    });
  } catch (error) {
    if (error instanceof DOMException && error.name === "AbortError")
      throw error;
    throw new APIError("generic");
  }
  try {
    return await readProgressResponse<T>(response, {
      signal: options.signal,
      onProgress: options.onProgress,
    });
  } catch (error) {
    if (!(error instanceof ProgressError)) throw error;
    if (error.status === 401 && path !== "/login" && path !== "/session")
      window.dispatchEvent(new Event("ps:session-expired"));
    throw new APIError(error.code, error.status);
  }
}
export type Session = { csrf_token: string; organization: string };
export type Policy = {
  version: number;
  mask_pii: boolean;
  block_secrets: boolean;
  contextual_pii: boolean;
  blocked_keywords: string[];
};
export type Decision = "allowed" | "masked" | "blocked" | "failed";
export type AuditEvent = {
  id: string;
  decision: Decision;
  reason: string;
  entity_count: number;
  entity_types: string[];
  latency_ms: number;
  created_at: string;
};
export type APIKey = {
  id: string;
  name: string;
  prefix: string;
  active: boolean;
  created_at: string;
};
export type Overview = {
  engine_ready: boolean;
  contextual_ready: boolean;
  mode: string;
  counts: Record<Decision, number>;
  sample_size: number;
  sample_limit: number;
  openai_configured: boolean;
  anthropic_configured: boolean;
  gemini_configured: boolean;
  base_url: string;
};
export type Inspection = {
  sanitized_text: string;
  is_blocked: boolean;
  reason_code: string;
  entity_types: string[] | null;
  entity_count: number;
  processing_time_micros: number;
};
