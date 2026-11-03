import { useState } from "react";
import { useQuery } from "@tanstack/react-query";
import { LuRefreshCw } from "react-icons/lu";
import { request, type AuditEvent } from "../../app/api";
import { Empty, ErrorNotice, EventTable, Heading } from "../../components/ui";
import { usePreferences } from "../preferences/Preferences";

export function EventsScreen() {
  const { t } = usePreferences();
  const [offset, setOffset] = useState(0);
  const [decision, setDecision] = useState("");
  const events = useQuery({
    queryKey: ["events", offset, decision],
    queryFn: ({ signal }) =>
      request<AuditEvent[]>(`/events?offset=${offset}&decision=${decision}`, {
        signal,
      }),
  });
  return (
    <>
      <Heading
        title={t("events.title")}
        description={t("events.description")}
        action={
          <button
            className="button secondary"
            disabled={events.isFetching}
            onClick={() => void events.refetch()}
          >
            <LuRefreshCw size={16} />
            {t("common.refresh")}
          </button>
        }
      />
      <ErrorNotice error={events.error} />
      <div className="field">
        <label htmlFor="decision-filter">{t("common.status")}</label>
        <select
          id="decision-filter"
          value={decision}
          onChange={(e) => {
            setDecision(e.target.value);
            setOffset(0);
          }}
        >
          <option value="">{t("events.all")}</option>
          {(["allowed", "masked", "blocked", "failed"] as const).map(
            (value) => (
              <option key={value} value={value}>
                {t(`decision.${value}`)}
              </option>
            ),
          )}
        </select>
      </div>
      <section className="panel">
        {events.isPending ? (
          <Empty>{t("common.loading")}</Empty>
        ) : (
          <EventTable events={events.data ?? []} />
        )}
      </section>
      <div className="pagination">
        <button
          className="button secondary"
          disabled={offset === 0 || events.isFetching}
          onClick={() => setOffset(Math.max(0, offset - 50))}
        >
          {t("common.previous")}
        </button>
        <button
          className="button secondary"
          disabled={(events.data?.length ?? 0) < 50 || events.isFetching}
          onClick={() => setOffset(offset + 50)}
        >
          {t("common.next")}
        </button>
      </div>
    </>
  );
}
