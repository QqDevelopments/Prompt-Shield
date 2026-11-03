import { useState } from "react";
import { useQuery, useQueryClient } from "@tanstack/react-query";
import { request, type APIKey } from "../../app/api";
import {
  ConfirmDialog,
  CopyButton,
  Empty,
  ErrorNotice,
  Heading,
} from "../../components/ui";
import { usePreferences } from "../preferences/Preferences";

export function KeysScreen() {
  const { t, date } = usePreferences();
  const cache = useQueryClient();
  const keys = useQuery({
    queryKey: ["keys"],
    queryFn: ({ signal }) => request<APIKey[]>("/keys", { signal }),
  });
  const [name, setName] = useState("");
  const [secret, setSecret] = useState("");
  const [revoke, setRevoke] = useState<string | null>(null);
  const [busy, setBusy] = useState(false);
  const [error, setError] = useState<unknown>(null);
  const create = async () => {
    setBusy(true);
    setError(null);
    try {
      const result = await request<{ secret: string }>("/keys", {
        method: "POST",
        body: { name },
      });
      setSecret(result.secret);
      setName("");
      void cache.invalidateQueries({ queryKey: ["keys"] });
    } catch (e) {
      setError(e);
    } finally {
      setBusy(false);
    }
  };
  return (
    <>
      <Heading title={t("keys.title")} description={t("keys.description")} />
      <ErrorNotice error={error || keys.error} />
      {keys.isError && (
        <button
          className="button secondary"
          onClick={() => void keys.refetch()}
        >
          {t("common.refresh")}
        </button>
      )}
      {secret && (
        <section className="notice key-secret">
          <strong>{t("keys.once")}</strong>
          <code>{secret}</code>
          <div className="actions">
            <CopyButton text={secret} />
            <button className="button secondary" onClick={() => setSecret("")}>
              {t("keys.dismiss")}
            </button>
          </div>
        </section>
      )}
      <form
        className="panel key-create"
        onSubmit={(event) => {
          event.preventDefault();
          void create();
        }}
      >
        <label htmlFor="key-name">{t("common.name")}</label>
        <div className="inline-form">
          <input
            id="key-name"
            placeholder={t("keys.namePlaceholder")}
            value={name}
            onChange={(e) => setName(e.target.value)}
            maxLength={80}
            required
            disabled={busy}
          />
          <button
            className="button primary"
            disabled={busy || !name.trim() || !!secret}
          >
            {t("keys.create")}
          </button>
        </div>
      </form>
      <section className="panel">
        {keys.data?.length ? (
          <div className="table-wrap">
            <table>
              <thead>
                <tr>
                  <th>{t("common.name")}</th>
                  <th>{t("common.status")}</th>
                  <th>{t("common.date")}</th>
                  <th>
                    <span className="sr-only">{t("keys.revoke")}</span>
                  </th>
                </tr>
              </thead>
              <tbody>
                {keys.data.map((key) => (
                  <tr key={key.id}>
                    <td>
                      <strong>{key.name}</strong>
                      <small className="key-prefix">{key.prefix}…</small>
                    </td>
                    <td>
                      {t(key.active ? "common.active" : "common.revoked")}
                    </td>
                    <td className="muted">{date(key.created_at)}</td>
                    <td>
                      {key.active && (
                        <button
                          className="text-link danger-text"
                          onClick={() => setRevoke(key.id)}
                        >
                          {t("keys.revoke")}
                        </button>
                      )}
                    </td>
                  </tr>
                ))}
              </tbody>
            </table>
          </div>
        ) : (
          <Empty>{t(keys.isPending ? "common.loading" : "keys.empty")}</Empty>
        )}
      </section>
      <ConfirmDialog
        open={!!revoke}
        title={t("keys.confirmTitle")}
        busy={busy}
        onClose={() => setRevoke(null)}
        onConfirm={async () => {
          if (!revoke) return;
          setBusy(true);
          setError(null);
          try {
            await request(`/keys/${encodeURIComponent(revoke)}`, {
              method: "DELETE",
            });
            setRevoke(null);
            void cache.invalidateQueries({ queryKey: ["keys"] });
          } catch (e) {
            setError(e);
          } finally {
            setBusy(false);
          }
        }}
      >
        {t("keys.confirmDescription")}
      </ConfirmDialog>
    </>
  );
}
