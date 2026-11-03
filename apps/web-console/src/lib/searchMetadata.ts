import { AppConstants } from "../constants/appConstants.ts";
import en from "../localization/en.json" with { type: "json" };
import tr from "../localization/tr.json" with { type: "json" };
import { type PublicLocale } from "./publicEntry.ts";

export type SearchTag = {
  tag: string;
  attrs: Record<string, string>;
  injectTo: "head";
};

export function searchMetadata(
  command: "build" | "serve",
  vercelEnvironment?: string,
  locale: PublicLocale = "en",
) {
  const indexable =
    command === "build" &&
    (!vercelEnvironment || vercelEnvironment === "production");
  const canonical = new URL(`/${locale}`, AppConstants.siteUrl).href;
  const dictionary = locale === "tr" ? tr : en;
  const languages = {
    en: `${AppConstants.siteUrl}/en`,
    tr: `${AppConstants.siteUrl}/tr`,
    "x-default": `${AppConstants.siteUrl}/en`,
  };
  return {
    tags: [
      ...Object.entries(languages).map(([hreflang, href]) => ({
        tag: "link",
        attrs: { rel: "alternate", hreflang, href },
        injectTo: "head" as const,
      })),
      {
        tag: "meta",
        attrs: {
          name: "description",
          content: dictionary["public.description"],
        },
        injectTo: "head" as const,
      },
      {
        tag: "link",
        attrs: { rel: "canonical", href: canonical },
        injectTo: "head" as const,
      },
      {
        tag: "meta",
        attrs: { property: "og:url", content: canonical },
        injectTo: "head" as const,
      },
      {
        tag: "meta",
        attrs: {
          name: "robots",
          content: indexable ? "index, follow" : "noindex, nofollow",
        },
        injectTo: "head" as const,
      },
    ] as SearchTag[],
    // Crawlers must be able to read the noindex response on protected pages.
    robots: `User-agent: *\nAllow: /\n${indexable ? `\nSitemap: ${AppConstants.siteUrl}/sitemap.xml\n` : ""}`,
    sitemap: `<?xml version="1.0" encoding="UTF-8"?>\n<urlset xmlns="http://www.sitemaps.org/schemas/sitemap/0.9" xmlns:xhtml="http://www.w3.org/1999/xhtml">${
      indexable
        ? Object.values(languages)
            .slice(0, 2)
            .map(
              (url) =>
                `<url><loc>${url}</loc>${Object.entries(languages)
                  .map(
                    ([language, target]) =>
                      `<xhtml:link rel="alternate" hreflang="${language}" href="${target}" />`,
                  )
                  .join("")}</url>`,
            )
            .join("\n")
        : ""
    }</urlset>\n`,
  };
}

function escapeHTML(value: string): string {
  return value
    .replaceAll("&", "&amp;")
    .replaceAll("<", "&lt;")
    .replaceAll('"', "&quot;");
}

export function localizedPublicHTML(
  html: string,
  locale: PublicLocale,
  environment?: string,
): string {
  const metadata = searchMetadata("build", environment, locale);
  const dictionary = locale === "tr" ? tr : en;
  const tags = metadata.tags
    .map(
      ({ tag, attrs }) =>
        `<${tag} ${Object.entries(attrs)
          .map(([key, value]) => `${key}="${escapeHTML(String(value))}"`)
          .join(" ")}>`,
    )
    .join("\n");
  return html
    .replace(/<html\b[^>]*>/i, `<html lang="${locale}">`)
    .replace(
      /<title>.*?<\/title>/is,
      `<title>Prompt Shield · ${escapeHTML(dictionary["public.title"])}</title>`,
    )
    .replace(
      /<meta\b[^>]*(?:name="(?:description|robots)"|property="og:url")[^>]*>/gi,
      "",
    )
    .replace(/<link\b[^>]*rel="(?:canonical|alternate)"[^>]*>/gi, "")
    .replace("</head>", `${tags}\n</head>`)
    .replace(
      /<div id="root"><\/div>/,
      `<div id="root"><main><h1>${escapeHTML(dictionary["public.title"])}</h1><p>${escapeHTML(dictionary["public.description"])}</p><nav aria-label="${escapeHTML(dictionary["preferences.language"])}"><a href="/en">English</a> <a href="/tr">Türkçe</a></nav></main></div>`,
    );
}
