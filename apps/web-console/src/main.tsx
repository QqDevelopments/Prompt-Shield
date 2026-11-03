import React from "react";
import ReactDOM from "react-dom/client";
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { App } from "./app/App";
import { ErrorBoundary } from "./app/ErrorBoundary";
import { PreferencesProvider } from "./features/preferences/Preferences";
import "./styles/app.css";
import { loadRuntime, productMode } from "./app/runtime";
import { PublicScreen } from "./features/public/PublicScreen";
const queryClient = new QueryClient({
  defaultOptions: {
    queries: {
      retry: false,
      staleTime: 10000,
      gcTime: 60000,
      refetchOnWindowFocus: false,
    },
  },
});
const root = document.getElementById("root");
if (!root) throw new Error("Application root missing");
const reactRoot = ReactDOM.createRoot(root);
void loadRuntime()
  .then(() => {
    if (productMode() !== "community")
      throw new Error("product_bundle_mismatch");
    return reactRoot.render(
      <React.StrictMode>
        <PreferencesProvider>
          <QueryClientProvider client={queryClient}>
            <ErrorBoundary>
              {location.pathname === "/admin" ||
              location.pathname.startsWith("/admin/") ? (
                <App />
              ) : (
                <PublicScreen />
              )}
            </ErrorBoundary>
          </QueryClientProvider>
        </PreferencesProvider>
      </React.StrictMode>,
    );
  })
  .catch(() =>
    reactRoot.render(
      <p role="alert">
        Unable to load application configuration. / Uygulama yapılandırması
        yüklenemedi.
      </p>,
    ),
  );
