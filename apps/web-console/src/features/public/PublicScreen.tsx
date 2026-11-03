import {
  useCallback,
  useEffect,
  useRef,
  useState,
  type FormEvent,
} from "react";
import { LuInfo, LuScanLine, LuShield } from "react-icons/lu";
import {
  readProgressResponse,
  ProgressError,
  type RequestProgress,
} from "@qqdevelopments/prompt-shield-web-core";
import { APIError, type Inspection } from "../../app/api";
import { publicEntryPath } from "../../lib/publicEntry";
import { apiURL } from "../../app/runtime";
import { Badge, CopyButton, ErrorNotice } from "../../components/ui";
import { PreferenceControls, usePreferences } from "../preferences/Preferences";
import {
  providerChatPaths,
  providerSession,
  type Provider,
} from "./providerSession";
import { ModelPicker } from "./ModelPicker";

type Message = { role: "user" | "assistant"; content: string };

export function PublicScreen() {
  const { t, label, number, locale } = usePreferences();
  const [provider, setProvider] = useState<Provider>(
    () => providerSession.status().provider ?? "openai",
  );
  const [status, setStatus] = useState(() => providerSession.status());
  const [key, setKey] = useState("");
  const [model, setModel] = useState("");
  const [text, setText] = useState("");
  const [messages, setMessages] = useState<Message[]>([]);
  const [inspection, setInspection] = useState<Inspection | null>(null);
  const [keyError, setKeyError] = useState<unknown>(null);
  const [chatError, setChatError] = useState<unknown>(null);
  const [busy, setBusy] = useState(false);
  const [progress, setProgress] = useState<RequestProgress | null>(null);
  const active = useRef<AbortController | null>(null);
  useEffect(() => {
    const refresh = () => setStatus(providerSession.status());
    const timer = window.setInterval(refresh, 1000);
    window.addEventListener("pageshow", refresh);
    return () => {
      clearInterval(timer);
      window.removeEventListener("pageshow", refresh);
      active.current?.abort();
    };
  }, []);
  const forget = useCallback(() => {
    active.current?.abort();
    active.current = null;
    setBusy(false);
    setProgress(null);
    providerSession.forget();
    setKey("");
    setModel("");
    setKeyError(null);
    setStatus(providerSession.status());
  }, []);
  const rejectKey = useCallback(() => {
    forget();
    setKeyError(new APIError("provider_key_rejected"));
  }, [forget]);
  const save = (event: FormEvent) => {
    event.preventDefault();
    setKeyError(null);
    try {
      providerSession.save(provider, key);
      setKey("");
      setModel("");
      setStatus(providerSession.status());
    } catch {
      setKeyError(new APIError("invalid_provider_key"));
    }
  };
  const send = async (inspect: boolean) => {
    const controller = new AbortController();
    active.current = controller;
    setBusy(true);
    setProgress(null);
    setChatError(null);
    setInspection(null);
    try {
      const headers: Record<string, string> = {
        "Content-Type": "application/json",
        "X-Prompt-Shield-Progress": "v1",
      };
      if (!inspect) {
        try {
          headers["X-Provider-API-Key"] = providerSession.take(provider);
        } catch {
          throw new APIError("provider_key_required");
        }
      }
      const next: Message[] = [...messages, { role: "user", content: text }];
      const endpoint = inspect ? "inspect" : providerChatPaths[provider];
      const response = await fetch(apiURL(`/public/v1/${endpoint}`), {
        method: "POST",
        credentials: "omit",
        headers,
        signal: controller.signal,
        body: JSON.stringify(
          inspect ? { text } : { model, messages: next, max_tokens: 4096 },
        ),
      });
      let payload: unknown;
      try {
        payload = await readProgressResponse(response, {
          signal: controller.signal,
          onProgress: (value) => {
            if (active.current === controller && !controller.signal.aborted)
              setProgress(value);
          },
        });
      } catch (error) {
        if (error instanceof ProgressError) {
          if (error.code === "provider_key_rejected") forget();
          throw new APIError(error.code, error.status);
        }
        throw error;
      }
      if (inspect) {
        const result = payload as Inspection;
        if (!controller.signal.aborted) setInspection(result);
      } else {
        const body = payload as {
          choices?: { message?: { content?: string } }[];
          content?: { type: string; text?: string }[];
        };
        const reply =
          provider !== "anthropic"
            ? body.choices?.[0]?.message?.content
            : body.content
                ?.filter((b) => b.type === "text")
                .map((b) => b.text ?? "")
                .join("");
        if (typeof reply !== "string") throw new APIError("generic");
        if (controller.signal.aborted) return;
        setMessages([...next, { role: "assistant", content: reply }]);
        setText("");
      }
    } catch (e) {
      if (!controller.signal.aborted)
        setChatError(e instanceof APIError ? e : new APIError("generic"));
    } finally {
      if (active.current === controller) {
        active.current = null;
        setBusy(false);
        setProgress(null);
        setStatus(providerSession.status());
      }
    }
  };
  return (
    <div className="public-page">
      <a className="skip-link" href="#public-main">
        {t("nav.skip")}
      </a>
      <header className="topbar public-header">
        <a className="brand" href={publicEntryPath(locale)}>
          <span className="brand-mark" aria-hidden="true">
            <LuShield size={23} />
          </span>
          <span>
            <strong>Prompt Shield</strong>
            <small>Community</small>
          </span>
        </a>
        <div className="topbar-actions">
          <PreferenceControls />
          <a href="/admin">{t("public.admin")}</a>
        </div>
      </header>
      <main id="public-main" className="public-main" tabIndex={-1}>
        <div className="public-intro">
          <p className="eyebrow">PROMPT SHIELD / COMMUNITY</p>
          <h1>{t("public.title")}</h1>
          <p className="muted">{t("public.description")}</p>
        </div>
        <div className="public-workspace">
          <section
            className="panel public-panel public-task"
            aria-labelledby="conversation-title"
          >
            <div className="panel-heading">
              <h2 id="conversation-title">{t("public.conversation")}</h2>
              <LuScanLine size={20} aria-hidden="true" />
            </div>
            <p className="muted">{t("public.history")}</p>
            <div role="log" aria-live="polite">
              {messages.map((message, index) => (
                <article
                  className={`public-message message-${message.role}`}
                  key={index}
                >
                  <strong>
                    {t(
                      message.role === "user"
                        ? "public.you"
                        : "public.assistant",
                    )}
                  </strong>
                  <p>{message.content}</p>
                </article>
              ))}
            </div>
            <label htmlFor="public-prompt">{t("playground.input")}</label>
            <textarea
              id="public-prompt"
              rows={5}
              value={text}
              maxLength={131072}
              disabled={busy}
              placeholder={t("playground.placeholder")}
              onChange={(e) => {
                setText(e.target.value);
                setInspection(null);
              }}
            />
            <div className="actions">
              <button
                type="button"
                className="button secondary"
                disabled={busy}
                onClick={() => {
                  setText(t("public.exampleText1"));
                  setInspection(null);
                  setChatError(null);
                }}
              >
                {t("public.example1")}
              </button>
              <button
                type="button"
                className="button secondary"
                disabled={busy}
                onClick={() => {
                  setText(t("public.exampleText2"));
                  setInspection(null);
                  setChatError(null);
                }}
              >
                {t("public.example2")}
              </button>
              <button
                className="button primary"
                disabled={busy || !text.trim()}
                onClick={() => void send(true)}
              >
                {busy ? (
                  <span className="button-spinner" aria-hidden="true" />
                ) : (
                  <LuScanLine size={16} aria-hidden="true" />
                )}
                {t("public.inspect")}
              </button>
              <button
                className="button secondary"
                disabled={
                  busy ||
                  !text.trim() ||
                  !model.trim() ||
                  !status.available ||
                  messages.length >= 126
                }
                onClick={() => void send(false)}
              >
                {t("public.send")}
              </button>
              {busy && (
                <button
                  className="button secondary"
                  onClick={() => active.current?.abort()}
                >
                  {t("common.cancel")}
                </button>
              )}
              <button
                className="button secondary"
                disabled={busy}
                onClick={() => {
                  setMessages([]);
                  setInspection(null);
                  setChatError(null);
                  setText("");
                }}
              >
                {t("public.clear")}
              </button>
            </div>
            {busy && (
              <p className="working-indicator" role="status">
                {progress?.state === "queued"
                  ? t("request.queued", { position: progress.position })
                  : t(
                      progress?.state === "processing"
                        ? "request.processing"
                        : "request.submitting",
                    )}
              </p>
            )}
            <ErrorNotice error={chatError} />
            {inspection && (
              <div className="result" aria-live="polite">
                <strong>
                  {t(
                    inspection.is_blocked
                      ? "public.blocked"
                      : "public.inspected",
                  )}
                </strong>
                <Badge
                  decision={
                    inspection.is_blocked
                      ? "blocked"
                      : inspection.entity_count
                        ? "masked"
                        : "allowed"
                  }
                />
                <p className="result-reason">
                  {label(`reason.${inspection.reason_code}`)}
                </p>
                {!inspection.is_blocked && (
                  <pre>{inspection.sanitized_text}</pre>
                )}
                <div className="entity-tags">
                  {inspection.entity_types?.map((kind) => (
                    <span key={kind}>{label(`entity.${kind}`)}</span>
                  ))}
                </div>
                <div className="result-metrics">
                  <span>
                    {t("playground.entities", {
                      count: inspection.entity_count,
                    })}
                  </span>
                  <span>
                    {t("playground.latency")}:{" "}
                    {number(inspection.processing_time_micros / 1000)} ms
                  </span>
                  {!inspection.is_blocked && (
                    <CopyButton text={inspection.sanitized_text} />
                  )}
                </div>
              </div>
            )}
          </section>
          <section
            className="panel public-panel provider-panel"
            aria-labelledby="key-title"
          >
            <h2 id="key-title">{t("public.credentials")}</h2>
            <ErrorNotice error={keyError} />
            {!status.available && (
              <div className="notice public-optional-notice" role="status">
                <LuInfo size={18} />
                <div className="notice-body">
                  <p>{t("public.keyOptionalNotice")}</p>
                  <button
                    type="button"
                    className="text-link"
                    disabled={busy}
                    onClick={() => {
                      setText(t("public.exampleText1"));
                      setInspection(null);
                      setChatError(null);
                      const promptEl = document.getElementById("public-prompt");
                      promptEl?.focus();
                      promptEl?.scrollIntoView({ behavior: "smooth" });
                    }}
                  >
                    {t("public.keyOptionalHint")}
                  </button>
                </div>
              </div>
            )}
            <p className="muted">{t("public.storage")}</p>
            {status.memoryOnly && <p role="status">{t("public.memoryOnly")}</p>}
            <label htmlFor="provider">{t("public.provider")}</label>
            <select
              id="provider"
              value={provider}
              disabled={busy}
              onChange={(e) => {
                forget();
                setChatError(null);
                setProvider(e.target.value as Provider);
                setModel("");
                setMessages([]);
              }}
            >
              <option value="openai">OpenAI</option>
              <option value="anthropic">Anthropic</option>
              <option value="gemini">Google Gemini</option>
            </select>
            {status.available ? (
              <div className="actions">
                <p role="status">{t("public.keyReady")}</p>
                <button className="button secondary" onClick={forget}>
                  {t("public.forget")}
                </button>
              </div>
            ) : (
              <form onSubmit={save}>
                <label htmlFor="provider-key">{t("public.key")}</label>
                <input
                  id="provider-key"
                  type="password"
                  autoComplete="off"
                  value={key}
                  required
                  maxLength={4096}
                  onChange={(e) => setKey(e.target.value)}
                />
                <button className="button secondary" disabled={busy}>
                  {t("public.saveKey")}
                </button>
              </form>
            )}
            {status.available ? (
              <ModelPicker
                key={provider}
                provider={provider}
                value={model}
                onChange={setModel}
                onRejected={rejectKey}
                disabled={busy}
              />
            ) : (
              <p className="muted">{t("public.modelsNeedKey")}</p>
            )}
          </section>
        </div>
      </main>
    </div>
  );
}
