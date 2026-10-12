import "../styles/app.css";
import "../styles/fonts-mono.css";
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { RouterProvider } from "@tanstack/react-router";
import { MotionConfig } from "motion/react";
import { StrictMode } from "react";
import { createRoot } from "react-dom/client";
import { ApiError, basePath, setCsrf } from "../api/client";
import { qk } from "../api/hooks";
import { Atmosphere } from "../components/atmosphere";
import { ErrorBoundary } from "../components/error-boundary";
import { ToastProvider } from "../components/toast";
import { initI18n } from "../i18n";
import { adminDicts } from "../i18n/admin";
import { createAppRouter } from "./router";

const queryClient = new QueryClient({
  defaultOptions: {
    queries: {
      staleTime: 5_000,
      retry: (count, e) => !(e instanceof ApiError && [401, 403, 404].includes(e.status)) && count < 2,
      refetchOnWindowFocus: true,
    },
  },
});

const router = createAppRouter(queryClient);

window.addEventListener("cozy:unauthorized", () => {
  if (router.state.location.pathname === "/login") return;
  queryClient.clear();
  void router.navigate({ to: "/login", search: { next: router.state.location.href } });
});

// The framed Mini App is served at <base>/tg: Telegram hands the page init data, which
// the server swaps for a session cookie. Any failure falls through to the normal boot
// (the login shows); normal boot never breaks because of this.
async function bootMiniApp(): Promise<void> {
  try {
    if (!location.pathname.startsWith(`${basePath}/tg`)) return;
    const initData = (window as unknown as { Telegram?: { WebApp?: { initData?: string } } }).Telegram?.WebApp?.initData;
    if (!initData) return;
    const res = await fetch(`${basePath}/tg/session`, {
      method: "POST",
      credentials: "same-origin",
      headers: { "Content-Type": "application/json" },
      body: JSON.stringify({ init_data: initData }),
    });
    if (!res.ok) return;
    const body = (await res.json()) as { csrf_token?: string };
    if (body.csrf_token) setCsrf(body.csrf_token);
    await queryClient.invalidateQueries({ queryKey: qk.me });
  } catch {
    /* normal boot continues */
  }
}

// Dictionaries load before the first render: t() stays synchronous everywhere. Pages read
// their texts at render time and subscribe to the language themselves (see page() in
// router.tsx), so a switch redraws them without remounting anything.
void bootMiniApp()
  .then(() => initI18n(adminDicts))
  .then(() =>
    createRoot(document.getElementById("root")!).render(
      <StrictMode>
        <ErrorBoundary>
          <QueryClientProvider client={queryClient}>
            {/* Motion follows the system's "reduce motion" like the CSS animations do. */}
            <MotionConfig reducedMotion="user">
              <ToastProvider>
                <Atmosphere />
                <RouterProvider router={router} />
              </ToastProvider>
            </MotionConfig>
          </QueryClientProvider>
        </ErrorBoundary>
      </StrictMode>,
    ),
  );
