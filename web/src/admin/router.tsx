import type { QueryClient } from "@tanstack/react-query";
import { createRootRouteWithContext, createRoute, createRouter, lazyRouteComponent, Navigate, Outlet, redirect, type AsyncRouteComponent } from "@tanstack/react-router";
import { ApiError, basePath, setCsrf } from "../api/client";
import { meQuery } from "../api/hooks";
import { useLocale } from "../i18n";
import { NotFoundPage, PageLoading, RouteError } from "./route-states";
import { SETTINGS_TABS, TARIFF_TABS, TELEGRAM_TABS, USER_STATES, type SettingsSearch, type TariffsSearch, type TelegramSearch, type UsersSearch } from "./search";
import { Shell } from "./shell";

/**
 * A page loaded on demand: the first screen (login, the shell) does not carry the code of
 * every page. The page subscribes to the language itself, so a switch redraws it with its
 * form state and open drawers intact (texts are read at render time).
 */
function page<M extends Record<string, unknown>, K extends keyof M & string>(load: () => Promise<M>, name: K) {
  const Lazy = lazyRouteComponent(load, name) as AsyncRouteComponent<object>;
  const Page = () => {
    useLocale();
    return <Lazy />;
  };
  // The router warms the chunk up when a link is hovered (defaultPreload: "intent").
  return Object.assign(Page, { preload: Lazy.preload });
}

/**
 * Which section a path belongs to, for the grants check in `_app.beforeLoad`. On a reload
 * the query cache is empty and the nav fail-opens while `me` is pending, so a sub-admin
 * could land on a page its grants forbid and die on a 403: denied pages go home instead.
 * The owner always passes; the buttons keep their fail-open `can()` semantics.
 */
function sectionDenied(path: string, scopes: Record<string, string> | undefined): boolean {
  const has = (tag: string) => scopes?.[tag] === "r" || scopes?.[tag] === "w";
  if (path === "/admins" || path.startsWith("/admins/")) return true;
  if (path === "/backups" || path.startsWith("/backups/")) return true;
  if (path.startsWith("/settings/admin-bot")) return true;
  if (path.startsWith("/settings/api") || path.startsWith("/api-docs")) return !has("api-keys");
  if (path === "/") return false;
  if (path.startsWith("/users")) return !has("users");
  if (path.startsWith("/tariffs")) return !has("tariffs");
  if (path.startsWith("/inbounds")) return !has("inbounds");
  if (path.startsWith("/nodes") || path.startsWith("/network")) return !has("node");
  if (path.startsWith("/settings")) return !has("settings");
  if (path.startsWith("/telegram")) return !has("telegram");
  return false;
}

export function createAppRouter(queryClient: QueryClient) {
  const root = createRootRouteWithContext<{ queryClient: QueryClient }>()({ component: () => <Outlet /> });

  const login = createRoute({
    getParentRoute: () => root,
    path: "/login",
    component: page(() => import("./pages/login"), "LoginPage"),
    validateSearch: (s: Record<string, unknown>): { next?: string } => ({ next: typeof s.next === "string" ? s.next : undefined }),
  });

  const app = createRoute({
    getParentRoute: () => root,
    id: "_app",
    component: Shell,
    beforeLoad: async ({ context, location }) => {
      try {
        const me = await context.queryClient.ensureQueryData(meQuery);
        setCsrf(me.csrf_token);
        if (!me.admin.owner) {
          // location.pathname carries the <base href> prefix; the map below does not.
          const path = basePath && location.pathname.startsWith(basePath) ? location.pathname.slice(basePath.length) || "/" : location.pathname;
          if (sectionDenied(path, me.admin.scopes as Record<string, string> | undefined)) {
            throw redirect({ to: "/" });
          }
        }
      } catch (e) {
        if (e instanceof ApiError && e.status === 401) {
          throw redirect({ to: "/login", search: { next: location.href } });
        }
        throw e;
      }
    },
  });

  const dashboard = createRoute({ getParentRoute: () => app, path: "/", component: page(() => import("./pages/dashboard"), "Dashboard") });
  const users = createRoute({
    getParentRoute: () => app,
    path: "/users",
    component: page(() => import("./pages/users"), "UsersPage"),
    validateSearch: (s: Record<string, unknown>): UsersSearch => ({
      state: USER_STATES.includes(s.state as (typeof USER_STATES)[number]) ? (s.state as UsersSearch["state"]) : "all",
      q: typeof s.q === "string" ? s.q : "",
      user: typeof s.user === "number" ? s.user : Number(s.user) || undefined,
      create: s.create === true || s.create === "true" ? true : undefined,
    }),
  });
  const tariffs = createRoute({
    getParentRoute: () => app,
    path: "/tariffs",
    component: page(() => import("./pages/tariffs"), "TariffsPage"),
    validateSearch: (s: Record<string, unknown>): TariffsSearch => ({
      tab: TARIFF_TABS.includes(s.tab as TariffsSearch["tab"]) ? (s.tab as TariffsSearch["tab"]) : "tariffs",
    }),
  });
  const inbounds = createRoute({ getParentRoute: () => app, path: "/inbounds", component: page(() => import("./pages/inbounds"), "InboundsPage") });
  const nodes = createRoute({ getParentRoute: () => app, path: "/nodes", component: page(() => import("./pages/nodes"), "NodesPage") });
  const network = createRoute({ getParentRoute: () => app, path: "/network", component: page(() => import("./pages/network"), "NetworkPage") });
  const settings = createRoute({
    getParentRoute: () => app,
    path: "/settings",
    component: page(() => import("./pages/settings"), "SettingsPage"),
    validateSearch: (s: Record<string, unknown>): SettingsSearch => ({
      tab: SETTINGS_TABS.includes(s.tab as SettingsSearch["tab"]) ? (s.tab as SettingsSearch["tab"]) : "general",
    }),
  });
  const telegram = createRoute({
    getParentRoute: () => app,
    path: "/telegram",
    component: page(() => import("./pages/telegram"), "TelegramPage"),
    validateSearch: (s: Record<string, unknown>): TelegramSearch => ({
      tab: TELEGRAM_TABS.includes(s.tab as TelegramSearch["tab"]) ? (s.tab as TelegramSearch["tab"]) : "connect",
    }),
  });
  const apiDocs = createRoute({ getParentRoute: () => app, path: "/settings/api", component: page(() => import("./pages/api"), "ApiPage") });
  const adminBot = createRoute({ getParentRoute: () => app, path: "/settings/admin-bot", component: page(() => import("./pages/admin-bot"), "AdminBotPage") });
  const admins = createRoute({ getParentRoute: () => app, path: "/admins", component: page(() => import("./pages/admins"), "AdminsPage") });
  const backups = createRoute({ getParentRoute: () => app, path: "/backups", component: page(() => import("./pages/backups"), "BackupsPage") });
  // The API section lived in the sidebar until 0.4.2: old links land on its new place.
  const apiDocsOld = createRoute({ getParentRoute: () => app, path: "/api-docs", beforeLoad: () => { throw redirect({ to: "/settings/api" }); } });
  // The framed Mini App is served at <base>/tg: it boots the session, then lands home.
  const tg = createRoute({ getParentRoute: () => app, path: "/tg", component: () => <Navigate to="/" /> });

  const routeTree = root.addChildren([login, app.addChildren([dashboard, users, tariffs, inbounds, nodes, network, telegram, apiDocs, adminBot, apiDocsOld, settings, admins, backups, tg])]);
  return createRouter({
    routeTree,
    basepath: basePath || "/",
    context: { queryClient },
    defaultPreload: "intent",
    scrollRestoration: true,
    defaultErrorComponent: RouteError,
    defaultNotFoundComponent: NotFoundPage,
    // A page's code is usually cached after the first visit: the placeholder shows only
    // when loading really takes a moment, and then stays long enough not to flash.
    defaultPendingComponent: PageLoading,
    defaultPendingMs: 200,
    defaultPendingMinMs: 300,
  });
}

declare module "@tanstack/react-router" {
  interface Register {
    router: ReturnType<typeof createAppRouter>;
  }
}
