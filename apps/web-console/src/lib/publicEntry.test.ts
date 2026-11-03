import { describe, expect, it } from "vitest";
import {
  isPublicEntry,
  publicEntryLocale,
  publicEntryPath,
} from "./publicEntry";

describe("public language URLs", () => {
  it("resolves explicit languages and excludes private areas", () => {
    expect(publicEntryLocale("/tr")).toBe("tr");
    expect(publicEntryLocale("/en/")).toBe("en");
    expect(publicEntryLocale("/xx")).toBeNull();
    expect(isPublicEntry("/admin")).toBe(false);
    expect(isPublicEntry("/workspace/overview")).toBe(false);
    expect(isPublicEntry("/platform/applications")).toBe(false);
  });
  it("preserves unrelated query parameters and anchors", () => {
    expect(publicEntryPath("tr", "?campaign=review&lang=en", "#details")).toBe(
      "/tr?campaign=review#details",
    );
  });
});
