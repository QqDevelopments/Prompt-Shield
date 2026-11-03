import { afterEach, describe, expect, it, vi } from "vitest";
import {
  act,
  cleanup,
  fireEvent,
  render,
  screen,
} from "@testing-library/react";
import {
  dictionaries,
  PreferencesProvider,
  resolveLocale,
  resolveTheme,
  translate,
  usePreferences,
} from "./Preferences";
afterEach(() => {
  cleanup();
  localStorage.clear();
  vi.restoreAllMocks();
});
describe("preferences", () => {
  it("validates dictionary data and interpolation", () => {
    expect(Object.keys(dictionaries.en).length).toBeGreaterThan(0);
    for (const key of Object.keys(dictionaries.tr)) {
      expect(dictionaries.en).toHaveProperty(key);
    }
    expect(translate("tr", "public.keyOptionalNotice")).toContain("API");
    expect(translate("tr", "overview.sample", { count: 4 })).toContain("4");
    expect(translate("en", "playground.entities", { count: 1 })).toBe(
      "1 detection",
    );
    expect(translate("en", "playground.entities", { count: 2 })).toBe(
      "2 detections",
    );
  });
  it("resolves invalid saved values and browser regional languages", () => {
    expect(resolveLocale("invalid", ["fr-FR", "tr-TR"])).toBe("tr");
    expect(resolveLocale("en", ["tr"])).toBe("en");
    expect(resolveLocale(null, ["ja"])).toBe("en");
    expect(resolveTheme("invalid")).toBe("system");
  });
  it("changes language and appearance without losing form input", () => {
    let listener: () => void = () => {};
    const media = {
      matches: false,
      addEventListener: (_: string, fn: () => void) => {
        listener = fn;
      },
      removeEventListener: vi.fn(),
    };
    vi.stubGlobal("matchMedia", () => media);
    function Example() {
      const { t, setLocale, setTheme } = usePreferences();
      return (
        <>
          <input aria-label="draft" />
          <p>{t("nav.overview")}</p>
          <button onClick={() => setLocale("tr")}>Turkish</button>
          <button onClick={() => setTheme("system")}>System</button>
        </>
      );
    }
    render(
      <PreferencesProvider>
        <Example />
      </PreferencesProvider>,
    );
    fireEvent.change(screen.getByLabelText("draft"), {
      target: { value: "draft stays" },
    });
    fireEvent.click(screen.getByText("Turkish"));
    expect(screen.getByText("Genel bakış")).toBeTruthy();
    expect(document.documentElement.lang).toBe("tr");
    expect((screen.getByLabelText("draft") as HTMLInputElement).value).toBe(
      "draft stays",
    );
    expect(localStorage.getItem("ps.locale.v1")).toBe("tr");
    act(() => {
      media.matches = true;
      listener();
    });
    expect(document.documentElement.dataset.theme).toBe("dark");
  });
});
