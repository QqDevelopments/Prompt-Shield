import { useEffect, useRef, useState } from "react";
import { useQueryClient } from "@tanstack/react-query";
import { LuScanLine, LuShieldCheck } from "react-icons/lu";
import { type RequestProgress } from "@qqdevelopments/prompt-shield-web-core";
import { request, type Inspection } from "../../app/api";
import { Badge, ErrorNotice, Heading } from "../../components/ui";
import { usePreferences } from "../preferences/Preferences";

export function PlaygroundScreen() {
  const { t, number, label } = usePreferences();
  const queryClient = useQueryClient();
  const [text, setText] = useState("");
  const [result, setResult] = useState<Inspection | null>(null);
  const [busy, setBusy] = useState(false);
  const [progress, setProgress] = useState<RequestProgress | null>(null);
  const [error, setError] = useState<unknown>(null);
  const active = useRef<AbortController | null>(null);
  useEffect(() => () => active.current?.abort(), []);
  const clear = () => {
    active.current?.abort();
    active.current = null;
    setText("");
    setResult(null);
    setError(null);
    setBusy(false);
    setProgress(null);
  };
  const inspect = async () => {
    const controller = new AbortController();
    active.current?.abort();
    active.current = controller;
    setBusy(true);
    setProgress(null);
    setError(null);
    setResult(null);
    try {
      const data = await request<Inspection>("/inspect", {
        method: "POST",
        body: { text },
        signal: controller.signal,
        onProgress: (value) => {
          if (active.current === controller && !controller.signal.aborted)
            setProgress(value);
        },
      });
      if (!controller.signal.aborted) {
        setResult(data);
        void queryClient.invalidateQueries({ queryKey: ["events"] });
        void queryClient.invalidateQueries({ queryKey: ["overview"] });
      }
    } catch (e) {
      if (!controller.signal.aborted) setError(e);
    } finally {
      if (active.current === controller) {
        setBusy(false);
        active.current = null;
      }
    }
  };
  return (
    <>
      <Heading
        title={t("playground.title")}
        description={t("playground.description")}
      />
      <ErrorNotice error={error} />
      {busy && (
        <p role="status">
          {progress?.state === "queued"
            ? t("request.queued", { position: progress.position })
            : t(
                progress?.state === "processing"
                  ? "request.processing"
                  : "request.submitting",
              )}
        </p>
      )}
      <div className="lab-grid">
        <section className="panel lab-input">
          <div className="panel-heading">
            <label htmlFor="prompt">
              <h2>{t("playground.input")}</h2>
            </label>
            <button
              className="text-link"
              onClick={() => {
                setText(t("playground.exampleText"));
                setResult(null);
              }}
              disabled={busy}
            >
              {t("playground.example")}
            </button>
          </div>
          <textarea
            id="prompt"
            value={text}
            onChange={(e) => {
              setText(e.target.value);
              setResult(null);
            }}
            placeholder={t("playground.placeholder")}
            maxLength={32768}
            disabled={busy}
          />
          <div className="lab-footer">
            <button className="button secondary" onClick={clear}>
              {t("common.clear")}
            </button>
            <button
              className="button primary"
              disabled={!text.trim() || busy}
              onClick={() => void inspect()}
            >
              <LuScanLine size={17} />
              {t(busy ? "common.loading" : "playground.inspect")}
            </button>
          </div>
        </section>
        <section className="panel lab-output" aria-live="polite">
          <div className="panel-heading">
            <h2>{t("playground.output")}</h2>
            {result && (
              <Badge
                decision={
                  result.is_blocked
                    ? "blocked"
                    : result.entity_count
                      ? "masked"
                      : "allowed"
                }
              />
            )}
          </div>
          {result ? (
            <div className="result">
              <p className="result-reason">
                {label(`reason.${result.reason_code}`)}
              </p>
              {result.is_blocked ? (
                <div className="notice danger">{t("playground.blocked")}</div>
              ) : (
                <pre>{result.sanitized_text}</pre>
              )}
              <div className="entity-tags">
                {result.entity_types?.map((kind) => (
                  <span key={kind}>{label(`entity.${kind}`)}</span>
                ))}
              </div>
              <div className="result-metrics">
                <span>
                  {t("playground.entities", {
                    count: result.entity_count,
                  })}
                </span>
                <span>
                  {t("playground.latency")}:{" "}
                  {number(result.processing_time_micros / 1000)} ms
                </span>
              </div>
            </div>
          ) : (
            <div className="waiting">
              <div className="scan-icon">
                <LuScanLine size={36} />
              </div>
              <h3>{t("playground.waiting")}</h3>
              <p>{t("playground.waitingDescription")}</p>
            </div>
          )}
        </section>
      </div>
      <p className="privacy-note">
        <LuShieldCheck size={15} />
        {t("playground.transient")}
      </p>
    </>
  );
}
