import { useState } from "react";
import { useQuery, useQueryClient } from "@tanstack/react-query";
import { request, type Policy, type Overview } from "../../app/api";
import { Empty, ErrorNotice, Heading } from "../../components/ui";
import { usePreferences } from "../preferences/Preferences";

function PolicyEditor({
  initial,
  contextualReady,
}: {
  initial: Policy;
  contextualReady: boolean;
}) {
  const { t } = usePreferences();
  const cache = useQueryClient();
  const [policy, setPolicy] = useState(initial);
  const [keywords, setKeywords] = useState(initial.blocked_keywords.join("\n"));
  const [busy, setBusy] = useState(false);
  const [error, setError] = useState<unknown>(null);
  const [saved, setSaved] = useState(false);
  return (
    <form
      className="panel policy-panel"
      onChange={() => setSaved(false)}
      onSubmit={async (event) => {
        event.preventDefault();
        setBusy(true);
        setError(null);
        try {
          const result = await request<Policy>("/policy", {
            method: "PUT",
            body: {
              ...policy,
              blocked_keywords: keywords
                .split("\n")
                .map((x) => x.trim())
                .filter(Boolean),
            },
          });
          cache.setQueryData(["policy"], result);
          setPolicy(result);
          setSaved(true);
        } catch (e) {
          setError(e);
        } finally {
          setBusy(false);
        }
      }}
    >
      <ErrorNotice error={error} />
      {(["mask_pii", "block_secrets"] as const).map((key, index) => (
        <label className="setting-row" key={key}>
          <div>
            <strong>{t(index === 0 ? "policy.mask" : "policy.secrets")}</strong>
            <p>
              {t(
                index === 0
                  ? "policy.maskDescription"
                  : "policy.secretsDescription",
              )}
            </p>
          </div>
          <input
            type="checkbox"
            role="switch"
            checked={policy[key]}
            disabled={busy || key === "block_secrets"}
            onChange={(e) => setPolicy({ ...policy, [key]: e.target.checked })}
          />
        </label>
      ))}
      <div className="setting-row">
        <div>
          <strong>{t("policy.context")}</strong>
          <p>
            {t(
              contextualReady
                ? "policy.contextAvailable"
                : "policy.contextDescription",
            )}
          </p>
        </div>
        <input
          type="checkbox"
          role="switch"
          checked={policy.contextual_pii}
          disabled={busy || !contextualReady}
          onChange={(e) =>
            setPolicy({ ...policy, contextual_pii: e.target.checked })
          }
          aria-label={t("policy.context")}
        />
      </div>
      <div className="field">
        <label htmlFor="keywords">{t("policy.keywords")}</label>
        <p id="keywords-help">{t("policy.keywordsDescription")}</p>
        <textarea
          id="keywords"
          aria-describedby="keywords-help"
          rows={5}
          value={keywords}
          onChange={(e) => setKeywords(e.target.value)}
          disabled={busy}
        />
      </div>
      <p className="caption">{t("policy.note")}</p>
      <div className="actions">
        <span role="status">{saved && t("common.saved")}</span>
        <button
          type="button"
          className="button secondary"
          disabled={busy}
          onClick={() => {
            setPolicy(initial);
            setKeywords(initial.blocked_keywords.join("\n"));
            setSaved(false);
          }}
        >
          {t("common.reset")}
        </button>
        <button className="button primary" disabled={busy}>
          {t(busy ? "common.loading" : "common.save")}
        </button>
      </div>
    </form>
  );
}
export function PolicyScreen() {
  const { t } = usePreferences();
  const overview = useQuery({
    queryKey: ["overview"],
    queryFn: ({ signal }) => request<Overview>("/overview", { signal }),
  });
  const query = useQuery({
    queryKey: ["policy"],
    queryFn: ({ signal }) => request<Policy>("/policy", { signal }),
  });
  return (
    <>
      <Heading
        title={t("policy.title")}
        description={t("policy.description")}
      />
      <ErrorNotice error={query.error} onRetry={() => void query.refetch()} />
      {query.data ? (
        <PolicyEditor
          initial={query.data}
          contextualReady={overview.data?.contextual_ready ?? false}
        />
      ) : query.isPending ? (
        <Empty>{t("common.loading")}</Empty>
      ) : null}
    </>
  );
}
