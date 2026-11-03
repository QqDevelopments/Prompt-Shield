import { useEffect, useState, type FormEvent } from "react";
import { useQueryClient } from "@tanstack/react-query";
import {
  LuActivity,
  LuArrowRight,
  LuCable,
  LuKeyRound,
  LuLayoutDashboard,
  LuLogOut,
  LuScanLine,
  LuSettings2,
  LuShield,
  LuSlidersHorizontal,
} from "react-icons/lu";
import { request, setCSRF, type Session } from "./api";
import {
  PreferenceControls,
  usePreferences,
} from "../features/preferences/Preferences";
import { EventsScreen } from "../features/events/EventsScreen";
import { IntegrationScreen } from "../features/integration/IntegrationScreen";
import { KeysScreen } from "../features/keys/KeysScreen";
import { OverviewScreen } from "../features/overview/OverviewScreen";
import { PlaygroundScreen } from "../features/playground/PlaygroundScreen";
import { PolicyScreen } from "../features/policies/PolicyScreen";
import { PreferencesScreen } from "../features/preferences/PreferencesScreen";
import { ErrorNotice } from "../components/ui";
import { providerSession } from "../features/public/providerSession";

const screens = {
  overview: OverviewScreen,
  playground: PlaygroundScreen,
  policies: PolicyScreen,
  keys: KeysScreen,
  events: EventsScreen,
  integration: IntegrationScreen,
  preferences: PreferencesScreen,
};
type Route = keyof typeof screens;
const navigation = [
  { id: "overview", icon: LuLayoutDashboard },
  { id: "playground", icon: LuScanLine },
  { id: "policies", icon: LuSlidersHorizontal },
  { id: "keys", icon: LuKeyRound },
  { id: "events", icon: LuActivity },
  { id: "integration", icon: LuCable },
  { id: "preferences", icon: LuSettings2 },
] as const;
function getRoute(): Route {
  const value = location.hash.slice(1);
  return Object.hasOwn(screens, value) ? (value as Route) : "overview";
}
function Brand() {
  const { t } = usePreferences();
  return (
    <div className="brand">
      <span className="brand-mark">
        <LuShield size={25} strokeWidth={1.8} />
      </span>
      <div>
        <strong>Prompt Shield</strong>
        <small>{t("brand.subtitle")}</small>
      </div>
    </div>
  );
}
export function App() {
  const { t } = usePreferences();
  const cache = useQueryClient();
  const [session, setSession] = useState<Session | null>(null);
  const [loading, setLoading] = useState(true);
  const [route, setRoute] = useState<Route>(getRoute);
  const [password, setPassword] = useState("");
  const [busy, setBusy] = useState(false);
  const [error, setError] = useState<unknown>(null);
  useEffect(() => {
    const controller = new AbortController();
    request<Session>("/session", { signal: controller.signal })
      .then((value) => {
        setCSRF(value.csrf_token);
        setSession(value);
      })
      .catch(() => {})
      .finally(() => {
        if (!controller.signal.aborted) setLoading(false);
      });
    return () => controller.abort();
  }, []);
  useEffect(() => {
    const change = () => setRoute(getRoute());
    window.addEventListener("hashchange", change);
    return () => window.removeEventListener("hashchange", change);
  }, []);
  useEffect(() => {
    const expire = () => {
      providerSession.forget();
      setCSRF("");
      setSession(null);
      setPassword("");
      cache.clear();
    };
    window.addEventListener("ps:session-expired", expire);
    return () => window.removeEventListener("ps:session-expired", expire);
  }, [cache]);
  const login = async (event: FormEvent) => {
    event.preventDefault();
    setBusy(true);
    setError(null);
    try {
      const value = await request<Session>("/login", {
        method: "POST",
        body: { password },
      });
      setCSRF(value.csrf_token);
      setSession(value);
      setPassword("");
      cache.clear();
    } catch (e) {
      setError(e);
    } finally {
      setBusy(false);
    }
  };
  const logout = async () => {
    providerSession.forget();
    setBusy(true);
    setError(null);
    try {
      await request("/logout", { method: "POST" });
      setSession(null);
      setCSRF("");
      setPassword("");
      cache.clear();
    } catch (e) {
      setError(e);
    } finally {
      setBusy(false);
    }
  };
  if (loading)
    return (
      <div className="boot">
        <Brand />
        <p role="status">{t("common.loading")}</p>
      </div>
    );
  if (!session)
    return (
      <div className="login-page">
        <header>
          <Brand />
          <PreferenceControls />
        </header>
        <main className="login-main">
          <div className="login-story">
            <p className="eyebrow">PROMPT SHIELD / COMMUNITY</p>
            <h1>{t("login.title")}</h1>
            <p>{t("login.description")}</p>
            <div className="security-art" aria-hidden="true">
              <div className="orbit orbit-one" />
              <div className="orbit orbit-two" />
              <div className="shield-core">
                <LuShield size={74} strokeWidth={1} />
              </div>
              <span className="art-label art-label-one">[EMAIL_1]</span>
              <span className="art-label art-label-two">[IBAN_1]</span>
            </div>
            <span className="privacy-note">
              <LuShield size={16} />
              {t("login.private")}
            </span>
          </div>
          <form
            className="login-form panel"
            onSubmit={(event) => void login(event)}
          >
            <LuKeyRound size={28} />
            <h2>{t("login.submit")}</h2>
            <p>{t("login.hint")}</p>
            <ErrorNotice error={error} />
            <label htmlFor="password">{t("login.password")}</label>
            <input
              id="password"
              type="password"
              autoComplete="current-password"
              required
              value={password}
              onChange={(e) => setPassword(e.target.value)}
              maxLength={72}
              disabled={busy}
            />
            <button className="button primary" disabled={busy}>
              {t(busy ? "common.loading" : "login.submit")}
              <LuArrowRight size={17} />
            </button>
          </form>
        </main>
      </div>
    );
  const Screen = screens[route];
  return (
    <div className="app-shell">
      <a
        className="skip-link"
        href="#main-content"
        onClick={(event) => {
          event.preventDefault();
          document.getElementById("main-content")?.focus();
        }}
      >
        {t("nav.skip")}
      </a>
      <aside className="sidebar">
        <Brand />
        <p className="nav-section">{t("nav.workspace")}</p>
        <nav aria-label={t("nav.workspace")}>
          {navigation.map(({ id, icon: Icon }) => (
            <a
              href={`#${id}`}
              key={id}
              className={route === id ? "selected" : ""}
              aria-current={route === id ? "page" : undefined}
            >
              <Icon size={19} />
              <span>{t(`nav.${id}`)}</span>
              {route === id && <span className="nav-active-dot" />}
            </a>
          ))}
        </nav>
        <div className="sidebar-bottom">
          <div className="organization">
            <span className="avatar">
              {session.organization.slice(0, 1).toUpperCase()}
            </span>
            <span>
              <strong>{session.organization}</strong>
              <small>{t("brand.subtitle")}</small>
            </span>
          </div>
          <button
            className="signout"
            onClick={() => void logout()}
            disabled={busy}
          >
            <LuLogOut size={17} />
            {t("nav.signout")}
          </button>
        </div>
      </aside>
      <div className="workspace">
        <header className="topbar">
          <div className="breadcrumb">
            Prompt Shield<span>/</span>
            <strong>{t(`nav.${route}`)}</strong>
          </div>
          <div className="topbar-actions">
            <PreferenceControls />
            <button
              className="mobile-signout button secondary compact"
              aria-label={t("nav.signout")}
              onClick={() => void logout()}
              disabled={busy}
            >
              <LuLogOut size={16} />
            </button>
          </div>
        </header>
        <main id="main-content" className="main-content" tabIndex={-1}>
          <ErrorNotice error={error} />
          <Screen />
        </main>
        <footer className="workspace-footer">
          <span>Prompt Shield</span>
          <span>{t("brand.subtitle")} · v0.1.0</span>
        </footer>
      </div>
    </div>
  );
}
