import { expect, it } from "vitest";
import { parseRuntime } from "./runtime";
it("accepts portable API origins and rejects credentials and non-origin URLs", () => {
  expect(
    parseRuntime({
      apiBaseURL: "https://api.example.test/",
      productMode: "community",
    }).apiBaseURL,
  ).toBe("https://api.example.test");
  for (const apiBaseURL of [
    "https://key@example.test",
    "https://example.test/?key=secret",
    "http://remote.test",
    "https://example.test/path",
    "javascript:alert(1)",
  ]) {
    expect(() =>
      parseRuntime({ apiBaseURL, productMode: "community" }),
    ).toThrow();
  }
});
