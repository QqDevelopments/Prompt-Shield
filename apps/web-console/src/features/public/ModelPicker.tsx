import { useEffect, useState } from "react";
import { APIError } from "../../app/api";
import { apiURL } from "../../app/runtime";
import { ErrorNotice } from "../../components/ui";
import { usePreferences } from "../preferences/Preferences";
import { providerSession, type Provider } from "./providerSession";

type Model = { id: string; name: string };

export function ModelPicker({
  provider,
  value,
  onChange,
  onRejected,
  disabled,
}: {
  provider: Provider;
  value: string;
  onChange: (model: string) => void;
  onRejected: () => void;
  disabled: boolean;
}) {
  const { t } = usePreferences();
  const [models, setModels] = useState<Model[]>([]);
  const [loading, setLoading] = useState(true);
  const [error, setError] = useState<unknown>(null);
  const [revision, setRevision] = useState(0);
  useEffect(() => {
    const controller = new AbortController();
    let timedOut = false;
    const timer = window.setTimeout(() => {
      timedOut = true;
      controller.abort();
    }, 20000);
    const load = async () => {
      try {
        const response = await fetch(apiURL("/public/v1/models"), {
          method: "POST",
          credentials: "omit",
          cache: "no-store",
          headers: {
            "Content-Type": "application/json",
            "X-Provider-API-Key": providerSession.take(provider),
          },
          body: JSON.stringify({ provider }),
          signal: controller.signal,
        });
        if (!response.ok) {
          if (response.status === 404) {
            throw new APIError("models_endpoint_unavailable");
          }
          const body = await response.json().catch(() => ({}));
          throw new APIError(body.error?.code ?? "models_unavailable");
        }
        const body = (await response.json()) as { data?: Model[] };
        if (
          !Array.isArray(body.data) ||
          body.data.some(
            (m) => typeof m?.id !== "string" || typeof m?.name !== "string",
          )
        ) {
          throw new APIError("models_unavailable");
        }
        if (controller.signal.aborted) return;
        setModels(body.data);
        const preferred = {
          openai: /(?:^|-)luna(?:-|$)/i,
          anthropic: /(?:^|-)sonnet(?:-|$)/i,
          gemini: null,
        }[provider];
        onChange(
          preferred
            ? (body.data.find((m) => preferred.test(m.id))?.id ?? "")
            : "",
        );
      } catch (reason) {
        if (controller.signal.aborted && !timedOut) return;
        if (
          reason instanceof Error &&
          ["provider_key_rejected", "provider_key_required"].includes(
            reason.message,
          )
        ) {
          onRejected();
          return;
        }
        setError(
          reason instanceof APIError
            ? reason
            : new APIError("models_unavailable"),
        );
      } finally {
        window.clearTimeout(timer);
        if (!controller.signal.aborted || timedOut) setLoading(false);
      }
    };
    void load();
    return () => {
      window.clearTimeout(timer);
      controller.abort();
    };
  }, [provider, revision, onChange, onRejected]);

  return (
    <div>
      <label htmlFor="model">{t("public.model")}</label>
      <select
        id="model"
        value={value}
        disabled={disabled || loading || models.length === 0}
        onChange={(e) => onChange(e.target.value)}
        aria-describedby="model-hint"
      >
        <option value="">
          {t(loading ? "common.loading" : "public.selectModel")}
        </option>
        {models.map((model) => (
          <option key={model.id} value={model.id}>
            {model.name === model.id ? model.id : `${model.name} (${model.id})`}
          </option>
        ))}
      </select>
      <p className="muted" id="model-hint">
        {t(
          provider === "gemini"
            ? "public.geminiModelsHint"
            : "public.modelsHint",
        )}
      </p>
      {!loading && !error && models.length === 0 && (
        <p role="status">{t("public.noModels")}</p>
      )}
      <ErrorNotice error={error} />
      <button
        className="button secondary"
        disabled={disabled || loading}
        onClick={() => {
          onChange("");
          setModels([]);
          setError(null);
          setLoading(true);
          setRevision((r) => r + 1);
        }}
      >
        {t("public.refreshModels")}
      </button>
    </div>
  );
}
