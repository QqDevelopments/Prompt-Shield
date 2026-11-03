import type { IconType } from "react-icons";
import { LuSun, LuMoon, LuMonitor } from "react-icons/lu";
import { Heading } from "../../components/ui";
import { type Theme } from "./Preferences";
import { usePreferences } from "./Preferences";

export function PreferencesScreen() {
  const { t, locale, setLocale, theme, setTheme } = usePreferences();
  const themes: { value: Theme; icon: IconType }[] = [
    { value: "light", icon: LuSun },
    { value: "dark", icon: LuMoon },
    { value: "system", icon: LuMonitor },
  ];
  return (
    <>
      <Heading
        title={t("preferences.title")}
        description={t("preferences.description")}
      />
      <section className="panel preferences-panel">
        <div className="setting-row">
          <label htmlFor="preferred-language">
            <strong>{t("preferences.language")}</strong>
          </label>
          <select
            id="preferred-language"
            value={locale}
            onChange={(e) => setLocale(e.target.value === "tr" ? "tr" : "en")}
          >
            <option value="en">English</option>
            <option value="tr">Türkçe</option>
          </select>
        </div>
        <fieldset>
          <legend>{t("preferences.appearance")}</legend>
          <div className="theme-options">
            {themes.map(({ value, icon: Icon }) => (
              <label
                className={`theme-option ${theme === value ? "selected" : ""}`}
                key={value}
              >
                <input
                  type="radio"
                  name="appearance"
                  value={value}
                  checked={theme === value}
                  onChange={() => setTheme(value)}
                />
                <Icon size={28} />
                <strong>{t(`theme.${value}`)}</strong>
              </label>
            ))}
          </div>
          <p className="caption">{t("preferences.systemHint")}</p>
        </fieldset>
      </section>
    </>
  );
}
