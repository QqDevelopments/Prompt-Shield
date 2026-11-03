import assert from "node:assert/strict";
import test from "node:test";
import {
  localizedPublicHTML,
  searchMetadata,
} from "../src/lib/searchMetadata.ts";

test("production discovery lists both public language entries", () => {
  for (const environment of [undefined, "production"]) {
    const metadata = searchMetadata("build", environment);
    const canonical = metadata.tags.find((tag) => tag.attrs.rel === "canonical")
      ?.attrs.href;
    assert.equal(canonical, "https://prompt-shield.qqdevelopments.com/en");
    assert.equal(
      metadata.tags.find((tag) => tag.attrs.name === "robots")?.attrs.content,
      "index, follow",
    );
    assert.deepEqual(
      Array.from(
        metadata.sitemap.matchAll(/<loc>(.*?)<\/loc>/g),
        ([, url]) => url,
      ),
      [canonical, "https://prompt-shield.qqdevelopments.com/tr"],
    );
    assert.match(
      metadata.robots,
      /Sitemap: https:\/\/prompt-shield\.qqdevelopments\.com\/sitemap\.xml/,
    );
    assert.doesNotMatch(metadata.robots, /Disallow:/);
  }
});

test("preview and development outputs are noindex and publish no discovery URLs", () => {
  for (const [command, environment] of [
    ["serve", undefined],
    ["build", "preview"],
    ["build", "development"],
  ] as const) {
    const metadata = searchMetadata(command, environment);
    assert.equal(
      metadata.tags.find((tag) => tag.attrs.name === "robots")?.attrs.content,
      "noindex, nofollow",
    );
    assert.doesNotMatch(metadata.sitemap, /<loc>/);
    assert.doesNotMatch(metadata.robots, /Sitemap:/);
  }
});
test("localized HTML contains matching language, canonical, translated visible text and alternates", () => {
  const html =
    '<html lang="en"><head><title>Prompt Shield</title></head><body><div id="root"></div></body></html>';
  for (const locale of ["en", "tr"] as const) {
    const rendered = localizedPublicHTML(html, locale);
    assert.match(rendered, new RegExp('lang="' + locale + '"'));
    assert.match(
      rendered,
      new RegExp(
        'rel="canonical" href="https://prompt-shield.qqdevelopments.com/' +
          locale +
          '"',
      ),
    );
    for (const language of ["en", "tr", "x-default"])
      assert.match(rendered, new RegExp('hreflang="' + language + '"'));
    assert.match(rendered, /<h1>/);
    assert.match(rendered, /href="\/tr"/);
    assert.match(rendered, /index, follow/);
    assert.match(
      localizedPublicHTML(html, locale, "preview"),
      /noindex, nofollow/,
    );
  }
  assert.notEqual(
    localizedPublicHTML(html, "en"),
    localizedPublicHTML(html, "tr"),
  );
});
