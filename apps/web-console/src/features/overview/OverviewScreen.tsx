import { useQuery } from "@tanstack/react-query";
import type { IconType } from "react-icons";
import {
  LuShieldCheck,
  LuShieldAlert,
  LuFingerprint,
  LuScanLine,
  LuArrowUpRight,
  LuArrowRight,
} from "react-icons/lu";
import { request, type Overview, type AuditEvent } from "../../app/api";
import { Empty, ErrorNotice, EventTable, Heading } from "../../components/ui";
import { usePreferences } from "../preferences/Preferences";

export function OverviewScreen() {
  const { t, number } = usePreferences();
  const overview = useQuery({
    queryKey: ["overview"],
    queryFn: ({ signal }) => request<Overview>("/overview", { signal }),
  });
  const events = useQuery({
    queryKey: ["events", 0],
    queryFn: ({ signal }) => request<AuditEvent[]>("/events", { signal }),
  });
  const icons: Record<"allowed" | "masked" | "blocked" | "failed", IconType> = {
    allowed: LuShieldCheck,
    masked: LuFingerprint,
    blocked: LuShieldAlert,
    failed: LuScanLine,
  };
  return (
    <>
      <p className="eyebrow">{t("overview.eyebrow")}</p>
      <Heading
        title={t("overview.title")}
        description={t("overview.description")}
        action={
          <a className="button primary" href="#playground">
            {t("overview.inspect")}
            <LuArrowUpRight size={18} />
          </a>
        }
      />
      <ErrorNotice
        error={overview.error}
        onRetry={() => void overview.refetch()}
      />
      <div className="stats-grid">
        {(["allowed", "masked", "blocked", "failed"] as const).map(
          (decision) => {
            const Icon = icons[decision];
            return (
              <section className="stat" key={decision}>
                <div className="stat-label">
                  {t(`decision.${decision}`)}
                  <Icon size={19} />
                </div>
                <strong>
                  {overview.data ? number(overview.data.counts[decision]) : "—"}
                </strong>
                <span className={`stat-line ${decision}`} />
              </section>
            );
          },
        )}
      </div>
      <p className="caption">
        {t("overview.sample", {
          count: number(overview.data?.sample_size ?? 0),
        })}
      </p>
      <div className="status-banner">
        <LuShieldCheck size={26} />
        <div>
          <strong>
            {t(
              overview.data?.engine_ready
                ? "overview.ready"
                : "overview.offline",
            )}
          </strong>
          <p>
            {t(
              overview.data?.contextual_ready
                ? "overview.hybrid"
                : "overview.mode",
            )}{" "}
            ·{" "}
            {t(
              overview.data?.contextual_ready
                ? "overview.modelReady"
                : "overview.model",
            )}
          </p>
        </div>
        <span
          className={`connection-dot ${overview.data?.engine_ready ? "online" : ""}`}
        />
      </div>
      <section className="panel">
        <div className="panel-heading">
          <h2>{t("overview.recent")}</h2>
          <a className="text-link" href="#events">
            {t("nav.events")}
            <LuArrowRight size={16} />
          </a>
        </div>
        <ErrorNotice error={events.error} />
        {events.isPending ? (
          <Empty>{t("common.loading")}</Empty>
        ) : (
          <EventTable events={(events.data ?? []).slice(0, 5)} />
        )}
      </section>
    </>
  );
}
