import { Component, type ReactNode } from "react";
import { usePreferences } from "../features/preferences/Preferences";

function Fallback() {
  const { t } = usePreferences();
  return (
    <main className="boot">
      <p role="alert">{t("error.generic")}</p>
      <button className="button primary" onClick={() => location.reload()}>
        {t("common.refresh")}
      </button>
    </main>
  );
}

export class ErrorBoundary extends Component<
  { children: ReactNode },
  { failed: boolean }
> {
  state = { failed: false };
  static getDerivedStateFromError() {
    return { failed: true };
  }
  render() {
    return this.state.failed ? <Fallback /> : this.props.children;
  }
}
