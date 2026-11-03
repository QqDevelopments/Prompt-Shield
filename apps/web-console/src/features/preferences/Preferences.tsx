import {
  createContext,
  useContext,
  useEffect,
  useState,
  type ReactNode,
} from "react";
import { searchMetadata } from "../../lib/searchMetadata";
import {
  isPublicEntry,
  publicEntryLocale,
  publicEntryPath,
} from "../../lib/publicEntry";
import en from "../../localization/en.json" with { type: "json" };
import tr from "../../localization/tr.json" with { type: "json" };

import {
  resolveLocale,
  resolveTheme,
  type Locale,
  type Theme,
} from "@qqdevelopments/prompt-shield-web-core";
export {
  resolveLocale,
  resolveTheme,
  type Locale,
  type Theme,
} from "@qqdevelopments/prompt-shield-web-core";
export type TranslationKey = keyof typeof en;
export const dictionaries: {
  en: Record<TranslationKey, string>;
  tr: Partial<Record<TranslationKey, string>>;
} = {
  en,
  tr,
};
export function translate(
  locale: Locale,
  key: TranslationKey,
  parameters: Record<string, string | number> = {},
) {
  const category =
    typeof parameters.count === "number"
      ? new Intl.PluralRules(locale).select(parameters.count)
      : "";
  const plural = category ? `${key}_${category}` : "";
  const selected = Object.hasOwn(en, plural) ? (plural as TranslationKey) : key;
  const template = dictionaries[locale]?.[selected] ?? en[selected];
  return template.replace(/\{(\w+)\}/g, (match, name: string) =>
    typeof parameters[name] === "number"
      ? new Intl.NumberFormat(locale).format(parameters[name])
      : String(parameters[name] ?? match),
  );
}
function read(key: string) {
  try {
    return localStorage.getItem(key);
  } catch {
    return null;
  }
}
function write(key: string, value: string) {
  try {
    localStorage.setItem(key, value);
  } catch {
    /* Rendering does not depend on storage. */
  }
}
type PreferencesValue = {
  locale: Locale;
  theme: Theme;
  setLocale: (value: Locale) => void;
  setTheme: (value: Theme) => void;
  t: typeof translate extends (locale: Locale, ...args: infer A) => string
    ? (...args: A) => string
    : never;
  label: (key: string) => string;
  number: (value: number) => string;
  date: (value: string) => string;
};
const PreferencesContext = createContext<PreferencesValue | null>(null);
export function PreferencesProvider({ children }: { children: ReactNode }) {
  const [locale, updateLocale] = useState<Locale>(
    () =>
      publicEntryLocale(location.pathname) ??
      resolveLocale(read("ps.locale.v1"), navigator.languages),
  );
  const [theme, updateTheme] = useState<Theme>(() =>
    resolveTheme(read("ps.theme.v1")),
  );
  useEffect(() => {
    document.documentElement.lang = locale;
    document.documentElement.dir = "ltr";
    document.title = `Prompt Shield · ${translate(locale, isPublicEntry(location.pathname) ? "public.title" : "brand.subtitle")}`;
    if (publicEntryLocale(location.pathname)) {
      const environment = document
        .querySelector('meta[name="robots"]')
        ?.getAttribute("content")
        ?.includes("noindex")
        ? "preview"
        : "production";
      for (const { tag, attrs } of searchMetadata("build", environment, locale)
        .tags) {
        const selector = attrs.hreflang
          ? `link[hreflang="${attrs.hreflang}"]`
          : attrs.rel
            ? `link[rel="${attrs.rel}"]`
            : attrs.property
              ? `meta[property="${attrs.property}"]`
              : `meta[name="${attrs.name}"]`;
        let element = document.querySelector(selector);
        if (!element) {
          element = document.createElement(tag);
          document.head.append(element);
        }
        for (const [key, value] of Object.entries(attrs))
          element.setAttribute(key, value);
      }
    }
  }, [locale]);
  useEffect(() => {
    const media = matchMedia("(prefers-color-scheme: dark)");
    const apply = () => {
      document.documentElement.dataset.theme =
        theme === "system" ? (media.matches ? "dark" : "light") : theme;
    };
    apply();
    media.addEventListener("change", apply);
    return () => media.removeEventListener("change", apply);
  }, [theme]);
  useEffect(() => {
    const navigate = () => {
      const selected = publicEntryLocale(location.pathname);
      if (selected) updateLocale(selected);
    };
    window.addEventListener("popstate", navigate);
    const synchronize = (event: StorageEvent) => {
      if (event.key === "ps.theme.v1")
        updateTheme(resolveTheme(event.newValue));
      if (event.key === "ps.locale.v1" && !publicEntryLocale(location.pathname))
        updateLocale(resolveLocale(event.newValue, navigator.languages));
    };
    window.addEventListener("storage", synchronize);
    return () => {
      window.removeEventListener("storage", synchronize);
      window.removeEventListener("popstate", navigate);
    };
  }, []);
  return (
    <PreferencesContext.Provider
      value={{
        locale,
        theme,
        setLocale: (value) => {
          if (isPublicEntry(location.pathname))
            history.pushState(
              null,
              "",
              publicEntryPath(value, location.search, location.hash),
            );
          updateLocale(value);
          write("ps.locale.v1", value);
        },
        setTheme: (value) => {
          updateTheme(value);
          write("ps.theme.v1", value);
        },
        t: (key, parameters) => translate(locale, key, parameters),
        label: (key) =>
          Object.hasOwn(en, key)
            ? translate(locale, key as TranslationKey)
            : translate(locale, "error.generic"),
        number: (value) =>
          new Intl.NumberFormat(locale, { maximumFractionDigits: 2 }).format(
            value,
          ),
        date: (value) =>
          new Intl.DateTimeFormat(locale, {
            dateStyle: "medium",
            timeStyle: "short",
          }).format(new Date(value)),
      }}
    >
      {children}
    </PreferencesContext.Provider>
  );
}
export function usePreferences() {
  const value = useContext(PreferencesContext);
  if (!value) throw new Error("PreferencesProvider required");
  return value;
}
export function PreferenceControls() {
  const { locale, setLocale, theme, setTheme, t } = usePreferences();
  return (
    <div className="preference-controls">
      <label>
        <span className="sr-only">{t("preferences.language")}</span>
        <select
          aria-label={t("preferences.language")}
          value={locale}
          onChange={(e) => setLocale(e.target.value as Locale)}
        >
          <option value="en">English</option>
          <option value="tr">Türkçe</option>
        </select>
      </label>
      <label>
        <span className="sr-only">{t("preferences.appearance")}</span>
        <select
          aria-label={t("preferences.appearance")}
          value={theme}
          onChange={(e) => setTheme(e.target.value as Theme)}
        >
          {(["light", "dark", "system"] as const).map((value) => (
            <option key={value} value={value}>
              {t(`theme.${value}`)}
            </option>
          ))}
        </select>
      </label>
    </div>
  );
}
