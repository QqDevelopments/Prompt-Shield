import { useEffect, useRef, useState, type ReactNode } from "react";
import { LuCopy, LuCheck, LuCircleAlert, LuShieldCheck } from "react-icons/lu";
import { APIError, type AuditEvent, type Decision } from "../app/api";
import { usePreferences } from "../features/preferences/Preferences";

export function ErrorNotice({
  error,
  onRetry,
}: {
  error: unknown;
  onRetry?: () => void;
}) {
  const { label, t } = usePreferences();
  return error ? (
    <div className="notice danger" role="alert">
      <LuCircleAlert size={18} />
      {label(`error.${error instanceof APIError ? error.code : "generic"}`)}
      {onRetry && (
        <button className="button secondary compact" onClick={onRetry}>
          {t("common.refresh")}
        </button>
      )}
    </div>
  ) : null;
}
export function Heading({
  title,
  description,
  action,
}: {
  title: string;
  description: string;
  action?: ReactNode;
}) {
  return (
    <header className="page-heading">
      <div>
        <h1>{title}</h1>
        <p>{description}</p>
      </div>
      {action}
    </header>
  );
}
export function Empty({ children }: { children: ReactNode }) {
  return (
    <div className="empty">
      <LuShieldCheck size={32} />
      <p>{children}</p>
    </div>
  );
}
export function Badge({ decision }: { decision: Decision }) {
  const { t } = usePreferences();
  return (
    <span className={`badge ${decision}`}>
      <span className="status-dot" />
      {t(`decision.${decision}`)}
    </span>
  );
}
export function CopyButton({ text }: { text: string }) {
  const { t } = usePreferences();
  const [copied, setCopied] = useState(false);
  const [failed, setFailed] = useState(false);
  useEffect(() => {
    setCopied(false);
    setFailed(false);
  }, [text]);
  return (
    <>
      <button
        className="button secondary compact"
        onClick={async () => {
          try {
            await navigator.clipboard.writeText(text);
            setCopied(true);
            setFailed(false);
          } catch {
            setFailed(true);
          }
        }}
      >
        {copied ? <LuCheck size={15} /> : <LuCopy size={15} />}{" "}
        {t(copied ? "common.copied" : "common.copy")}
      </button>
      {failed && <span role="alert">{t("error.clipboard")}</span>}
    </>
  );
}
export function EventTable({ events }: { events: AuditEvent[] }) {
  const { t, date, number, label } = usePreferences();
  if (!events.length) return <Empty>{t("events.empty")}</Empty>;
  return (
    <div className="table-wrap">
      <table>
        <thead>
          <tr>
            <th>{t("common.status")}</th>
            <th>{t("events.reason")}</th>
            <th>{t("events.entities")}</th>
            <th>{t("events.duration")}</th>
            <th>{t("common.date")}</th>
          </tr>
        </thead>
        <tbody>
          {events.map((event) => (
            <tr key={event.id}>
              <td>
                <Badge decision={event.decision} />
              </td>
              <td>{label(`reason.${event.reason}`)}</td>
              <td>{number(event.entity_count)}</td>
              <td>{number(event.latency_ms)} ms</td>
              <td className="muted nowrap">{date(event.created_at)}</td>
            </tr>
          ))}
        </tbody>
      </table>
    </div>
  );
}
export function ConfirmDialog({
  open,
  title,
  children,
  onClose,
  onConfirm,
  busy,
}: {
  open: boolean;
  title: string;
  children: ReactNode;
  onClose: () => void;
  onConfirm: () => void;
  busy: boolean;
}) {
  const ref = useRef<HTMLDialogElement>(null);
  const { t } = usePreferences();
  useEffect(() => {
    if (open) ref.current?.showModal();
    else ref.current?.close();
  }, [open]);
  return (
    <dialog
      ref={ref}
      aria-labelledby="confirm-title"
      onCancel={(event) => {
        if (busy) event.preventDefault();
        else onClose();
      }}
      onClose={onClose}
    >
      <h2 id="confirm-title">{title}</h2>
      <p>{children}</p>
      <div className="actions">
        <button className="button secondary" onClick={onClose} disabled={busy}>
          {t("common.cancel")}
        </button>
        <button
          className="button destructive"
          onClick={onConfirm}
          disabled={busy}
        >
          {t("keys.revoke")}
        </button>
      </div>
    </dialog>
  );
}
