import { defineConfig } from "vitest/config";
import react from "@vitejs/plugin-react";
import {
  localizedPublicHTML,
  searchMetadata,
} from "./src/lib/searchMetadata.ts";
export default defineConfig(({ command }) => ({
  plugins: [
    react(),
    {
      name: "public-search-metadata",
      transformIndexHtml() {
        return searchMetadata(command, process.env.VERCEL_ENV).tags;
      },
      generateBundle: {
        order: "post",
        handler(_options, bundle) {
          const metadata = searchMetadata(command, process.env.VERCEL_ENV);
          const index = bundle["index.html"];
          if (!index || index.type !== "asset")
            throw new Error("Built HTML is missing");
          for (const locale of ["en", "tr"] as const) {
            this.emitFile({
              type: "asset",
              fileName: `${locale}/index.html`,
              source: localizedPublicHTML(
                String(index.source),
                locale,
                process.env.VERCEL_ENV,
              ),
            });
          }
          this.emitFile({
            type: "asset",
            fileName: "robots.txt",
            source: metadata.robots,
          });
          this.emitFile({
            type: "asset",
            fileName: "sitemap.xml",
            source: metadata.sitemap,
          });
        },
      },
    },
  ],
  server: {
    port: 5173,
    strictPort: true,
    proxy: {
      "/api": "http://127.0.0.1:8080",
      "/public": "http://127.0.0.1:8080",
      "/readyz": "http://127.0.0.1:8080",
    },
  },
  test: { environment: "jsdom", include: ["src/**/*.test.{ts,tsx}"] },
}));
