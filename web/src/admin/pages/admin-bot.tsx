import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { Link } from "@tanstack/react-router";
import { ArrowDown, ArrowUp, Bot, ChevronLeft, Link2, Megaphone, Plus, Send, Trash2 } from "lucide-react";
import { useState, type FormEvent } from "react";
import { basePath, csrf } from "../../api/client";
import { meQuery, qk, useNodes } from "../../api/hooks";
import { QueryBoundary } from "../../components/query";
import { Columns } from "../../components/tabs";
import { useToast } from "../../components/toast";
import { Button, Field, PageHeader, Pill, Segmented, Skeleton } from "../../components/ui";
import { Switch } from "../../components/switch";
import { t, tMaybe } from "../../i18n";
import { ago } from "../../lib/format";
import { useDraft } from "../../lib/draft";

type RouteMode = "direct" | "node" | "proxy";

type LinkedChat = {
  tg_id: number;
  username: string;
  first_name: string;
  admin_id: number;
  admin_username: string;
  is_owner: boolean;
  created_at: string;
};

type AdminBotView = {
  enabled: boolean;
  token_set: boolean;
  token_hint?: string;
  running: boolean;
  error?: string;
  bot?: { username: string; name: string };
  linked: LinkedChat[];
  route: { mode: RouteMode; node_id?: number; proxy?: string };
  broadcast?: { total: number; sent: number; failed: number; started: number; active: boolean };
  mini_app_url?: string;
  menu?: AdminBotMenu;
  menu_defaults?: AdminBotMenu;
};

type JoinRequest = { tg_id: number; username: string; first_name: string; login: string; requested_at: string };

type AdminBotAction = "stats" | "find" | "nodes" | "broadcast" | "requests" | "unlink" | "help" | "url" | "page";

type AdminBotMenuButton = {
  id: string;
  action: AdminBotAction;
  label: string;
  on: boolean;
  row: boolean;
  url?: string;
  text?: string;
};

type AdminBotMenu = {
  lang: "ru" | "en";
  buttons: AdminBotMenuButton[];
  texts: { menu: string; help: string };
};

async function raw(path: string, init?: RequestInit): Promise<Response> {
  // Absolute paths ignore the <base href="/<secret>/"> the server injects: without the
  // prefix the request lands on the domain root, where nothing is mounted (404).
  return fetch(basePath + path, { credentials: "same-origin", ...init });
}

function csrfHeader(): Record<string, string> {
  return csrf() ? { "X-CSRF-Token": csrf() } : {};
}

async function getView(): Promise<AdminBotView> {
  const res = await raw("/api/v1/admin-bot");
  if (!res.ok) throw await toError(res);
  return (await res.json()) as AdminBotView;
}

async function getRequests(): Promise<JoinRequest[]> {
  const res = await raw("/api/v1/admin-bot/requests");
  if (!res.ok) throw await toError(res);
  return ((await res.json()) as { requests: JoinRequest[] }).requests;
}

async function toError(res: Response): Promise<Error> {
  let code = "server";
  try {
    const body = (await res.json()) as { detail?: string };
    if (body.detail) code = body.detail;
  } catch {
    /* keep generic */
  }
  return new Error(tMaybe(`errors.api.${code}`) ?? code);
}

async function patchView(body: object): Promise<AdminBotView> {
  const res = await raw("/api/v1/admin-bot", { method: "PATCH", headers: { "Content-Type": "application/json", ...csrfHeader() }, body: JSON.stringify(body) });
  if (!res.ok) throw await toError(res);
  return (await res.json()) as AdminBotView;
}

const VIEW_KEY = ["admin-bot"] as const;
const REQUESTS_KEY = ["admin-bot", "requests"] as const;

function failText(e: unknown): string {
  return e instanceof Error ? e.message : t("errors.generic");
}

/**
 * The bot the admins drive the panel from: its token and way to Telegram, who may link
 * it (a code for the owner, approve/refuse for the rest), and broadcasts. Owner-only,
 * like the admins page; the server enforces it too.
 */
export function AdminBotPage() {
  const qc = useQueryClient();
  const me = useQuery(meQuery);
  const view = useQuery({
    queryKey: VIEW_KEY,
    queryFn: getView,
    refetchInterval: (q) => (q.state.data?.broadcast?.active ? 2_000 : 10_000),
  });
  if (me.data && !me.data.admin.owner) {
    // Same stale-grants recovery as the admins page: re-read me, then the view.
    const retry = async () => {
      await qc.invalidateQueries({ queryKey: qk.me });
      await view.refetch();
    };
    return (
      <>
        <PageHeader title={t("adminBot.title")} />
        <section className="card glass max-w-4xl">
          <div className="state-box" role="alert">
            <p className="text-[13px] text-[var(--berry-600)]">{t("errors.api.no_grant")}</p>
            <div className="mt-2 flex flex-wrap justify-center gap-2">
              <Button variant="primary" onClick={() => void retry()}>
                {t("common.retry")}
              </Button>
              <Button onClick={() => location.reload()}>{t("common.reload")}</Button>
              <Link to="/" className="btn btn-glass">
                {t("errors.toOverview")}
              </Link>
            </div>
          </div>
        </section>
      </>
    );
  }
  return (
    <>
      <Link to="/settings" search={{ tab: "security" }} className="mb-2 inline-flex items-center gap-1 rounded-lg text-[13px] font-medium text-[var(--ink-500)] hover:text-[var(--ink-900)]">
        <ChevronLeft size={16} aria-hidden /> {t("adminBot.back")}
      </Link>
      <PageHeader title={t("adminBot.title")} sub={t("adminBot.sub")} />
      <QueryBoundary query={view} pending={<Skeleton style={{ height: 320, borderRadius: 20 }} />} wrap={(state) => <section className="card glass">{state}</section>}>
        {(v) => <AdminBotBody v={v} />}
      </QueryBoundary>
    </>
  );
}

function statusOf(v: AdminBotView): { tone: "ok" | "warn" | "bad" | "off"; text: string } {
  if (!v.enabled) return { tone: "off", text: t("adminBot.off") };
  if (v.running && !v.error) return { tone: "ok", text: t("adminBot.running") };
  if (v.running) return { tone: "warn", text: t("adminBot.reconnecting") };
  if (!v.error) return { tone: "off", text: t("adminBot.starting") };
  return { tone: "bad", text: t("adminBot.stopped") };
}

function AdminBotBody({ v }: { v: AdminBotView }) {
  return (
    <Columns
      wide="left"
      left={
        <>
          <StatusCard v={v} />
          <SetupCard v={v} />
          <LinkCodeCard />
          <BroadcastCard v={v} />
          <MenuTextsCard v={v} />
        </>
      }
      right={
        <>
          <RequestsCard />
          <LinkedCard v={v} />
        </>
      }
    />
  );
}

function StatusCard({ v }: { v: AdminBotView }) {
  const qc = useQueryClient();
  const toast = useToast();
  const [busy, setBusy] = useState(false);
  const st = statusOf(v);
  const toggle = async (on: boolean) => {
    setBusy(true);
    try {
      qc.setQueryData(VIEW_KEY, await patchView({ enabled: on }));
    } catch (e) {
      toast.error(failText(e));
    } finally {
      setBusy(false);
    }
  };
  return (
    <section className="card glass reveal">
      <div className="card-head">
        <div>
          <h2 className="card-title">{t("adminBot.status")}</h2>
          <div className="card-sub">{t("adminBot.statusSub")}</div>
        </div>
        {v.token_set ? <Switch checked={v.enabled} label={t("adminBot.enabled")} disabled={busy} onChange={(on) => void toggle(on)} /> : null}
      </div>
      {v.token_set && v.bot ? (
        <div className="panel-soft flex items-center gap-3 p-3">
          <span className="grid h-10 w-10 place-items-center rounded-xl bg-[var(--hover)] text-[var(--ink-700)]" aria-hidden>
            <Bot size={20} />
          </span>
          <div className="min-w-0 flex-1">
            <a className="font-semibold text-[var(--ink-900)] hover:underline" href={`https://t.me/${encodeURIComponent(v.bot.username)}`} target="_blank" rel="noreferrer noopener">
              @{v.bot.username}
            </a>
            <div className="truncate text-xs text-[var(--ink-500)]">{v.bot.name}</div>
          </div>
          <Pill tone={st.tone}>{st.text}</Pill>
        </div>
      ) : null}
      {v.error && v.enabled ? (
        <p className="mt-3 text-[13px] text-[var(--berry-600)]" role="alert">
          {tMaybe(`adminBot.err.${v.error}`) ?? tMaybe(`telegram.err.${v.error}`) ?? v.error}
        </p>
      ) : null}
      {v.token_set ? (
        <p className="mt-3 text-xs text-[var(--ink-500)]">
          {t("adminBot.stats", { n: v.linked.length })}
          {" · "}
          {v.mini_app_url ? t("adminBot.miniAppOn") : t("adminBot.miniAppOff")}
        </p>
      ) : null}
    </section>
  );
}

function SetupCard({ v }: { v: AdminBotView }) {
  const qc = useQueryClient();
  const toast = useToast();
  const nodes = useNodes();
  const [token, setToken] = useState("");
  const [mode, setMode] = useState<RouteMode>(v.route.mode);
  const [nodeId, setNodeId] = useState(v.route.node_id ?? 0);
  const [proxy, setProxy] = useState("");
  const [error, setError] = useState("");
  const [busy, setBusy] = useState(false);
  const remote = (nodes.data ?? []).filter((n) => !n.local);
  const submit = async (e: FormEvent) => {
    e.preventDefault();
    setError("");
    setBusy(true);
    try {
      const route = mode === "node" ? { mode, node_id: nodeId } : mode === "proxy" ? { mode, ...(proxy.trim() ? { proxy: proxy.trim() } : {}) } : { mode };
      const next = await patchView({ ...(token.trim() ? { token: token.trim() } : {}), route });
      qc.setQueryData(VIEW_KEY, next);
      setToken("");
      setProxy("");
      toast.ok(t("settings.saved"));
    } catch (err) {
      setError(failText(err));
    } finally {
      setBusy(false);
    }
  };
  return (
    <section className="card glass reveal">
      <div className="card-head">
        <div>
          <h2 className="card-title">{t("adminBot.setup")}</h2>
          <div className="card-sub">{t("adminBot.setupSub")}</div>
        </div>
      </div>
      <form onSubmit={(e) => void submit(e)} noValidate>
        {!v.token_set ? (
          <ol className="mb-4 flex list-decimal flex-col gap-1 pl-5 text-[13px] text-[var(--ink-600)]">
            <li>{t("adminBot.step1")}</li>
            <li>{t("adminBot.step2")}</li>
          </ol>
        ) : null}
        <Field label={t("adminBot.token")} htmlFor="ab-token" hint={v.token_hint ?? t("adminBot.tokenHint")} error={error}>
          <input
            id="ab-token"
            className="input mono"
            type="password"
            autoComplete="off"
            spellCheck={false}
            value={token}
            onChange={(e) => setToken(e.target.value)}
            placeholder={v.token_set ? t("adminBot.tokenKeep") : "123456789:AAH…"}
            aria-invalid={!!error}
          />
        </Field>
        <Field label={t("adminBot.route")}>
          <Segmented
            label={t("adminBot.route")}
            value={mode}
            onChange={setMode}
            options={[
              { value: "direct", label: t("adminBot.routeDirect") },
              { value: "node", label: t("adminBot.routeNode") },
              { value: "proxy", label: t("adminBot.routeProxy") },
            ]}
          />
        </Field>
        {mode === "node" ? (
          nodes.isPending ? (
            <Skeleton style={{ height: 40, borderRadius: 12, maxWidth: 320 }} />
          ) : remote.length === 0 ? (
            <div className="banner warn mb-4 flex-wrap" role="status">
              <span className="min-w-0 flex-1">{t("adminBot.routeNoNodes")}</span>
              <Link to="/nodes" className="btn btn-glass btn-sm">
                {t("adminBot.routeOpenNodes")}
              </Link>
            </div>
          ) : (
            <Field label={t("adminBot.routeNodeLabel")} htmlFor="ab-route-node">
              <select id="ab-route-node" className="input max-w-[320px]" value={nodeId} onChange={(e) => setNodeId(Number(e.target.value))}>
                <option value={0} disabled>
                  {t("adminBot.routeNodePick")}
                </option>
                {remote.map((n) => (
                  <option key={n.id} value={n.id}>
                    {n.name}
                  </option>
                ))}
              </select>
            </Field>
          )
        ) : null}
        {mode === "proxy" ? (
          <Field label={t("adminBot.routeProxyLabel")} htmlFor="ab-route-proxy" hint={v.route.proxy ? t("adminBot.routeProxyKeep", { proxy: v.route.proxy }) : t("adminBot.routeProxyHint")}>
            <input
              id="ab-route-proxy"
              className="input mono"
              type="text"
              autoComplete="off"
              spellCheck={false}
              value={proxy}
              onChange={(e) => setProxy(e.target.value)}
              placeholder="socks5://user:pass@203.0.113.5:1080"
              maxLength={512}
            />
          </Field>
        ) : null}
        <Button variant="primary" type="submit" loading={busy}>
          <Link2 size={16} aria-hidden /> {t("common.save")}
        </Button>
      </form>
    </section>
  );
}

function LinkCodeCard() {
  const toast = useToast();
  const [code, setCode] = useState<{ code: string; expires_at: string } | null>(null);
  const make = useMutation({
    mutationFn: async () => {
      const res = await raw("/api/v1/admin-bot/link-code", { method: "POST", headers: { ...csrfHeader() } });
      if (!res.ok) throw await toError(res);
      return (await res.json()) as { code: string; expires_at: string };
    },
    onSuccess: (c) => setCode(c),
    onError: (e) => toast.error(failText(e)),
  });
  return (
    <section className="card glass reveal">
      <div className="card-head">
        <div>
          <h2 className="card-title">{t("adminBot.link")}</h2>
          <div className="card-sub">{t("adminBot.linkSub")}</div>
        </div>
        <Button size="sm" variant="primary" loading={make.isPending} onClick={() => make.mutate()}>
          {t("adminBot.linkButton")}
        </Button>
      </div>
      {code ? (
        <p className="text-[13px]" aria-live="polite">
          <span className="mono text-base font-semibold tracking-[0.2em]">{code.code}</span> · {t("adminBot.linkExpires", { when: ago(code.expires_at) })}
        </p>
      ) : (
        <p className="text-xs text-[var(--ink-500)]">{t("adminBot.linkHint")}</p>
      )}
    </section>
  );
}

function RequestsCard() {
  const qc = useQueryClient();
  const toast = useToast();
  const reqs = useQuery({ queryKey: REQUESTS_KEY, queryFn: getRequests, refetchInterval: 10_000 });
  const act = useMutation({
    mutationFn: async ({ id, ok }: { id: number; ok: boolean }) => {
      const res = await raw(`/api/v1/admin-bot/requests/${id}/${ok ? "approve" : "refuse"}`, { method: "POST", headers: { ...csrfHeader() } });
      if (!res.ok) throw await toError(res);
    },
    onSuccess: () => {
      void qc.invalidateQueries({ queryKey: REQUESTS_KEY });
      void qc.invalidateQueries({ queryKey: VIEW_KEY });
    },
    onError: (e) => toast.error(failText(e)),
  });
  return (
    <section className="card glass reveal" aria-label={t("adminBot.requests")}>
      <div className="card-head">
        <div>
          <h2 className="card-title">{t("adminBot.requests")}</h2>
          <div className="card-sub">{t("adminBot.requestsSub")}</div>
        </div>
      </div>
      <QueryBoundary query={reqs} pending={<Skeleton style={{ height: 96 }} />}>
        {(list) =>
          list.length === 0 ? (
            <p className="py-4 text-center text-[13px] text-[var(--ink-500)]">{t("adminBot.requestsEmpty")}</p>
          ) : (
            <ul className="row-list">
              {list.map((r) => (
                <li key={r.tg_id} className="flex flex-wrap items-center justify-between gap-3 py-3">
                  <div className="min-w-0">
                    <div className="truncate text-[13px] font-medium">
                      {r.first_name} {r.username ? <span className="text-[var(--ink-500)]">@{r.username}</span> : null}
                    </div>
                    <div className="num text-xs text-[var(--ink-500)]">
                      {r.tg_id} · {t("adminBot.claimedLogin", { login: r.login })} · {ago(r.requested_at)}
                    </div>
                  </div>
                  <div className="flex gap-1">
                    <Button size="sm" variant="primary" loading={act.isPending && act.variables?.id === r.tg_id && act.variables.ok} onClick={() => act.mutate({ id: r.tg_id, ok: true })}>
                      {t("adminBot.approve")}
                    </Button>
                    <Button size="sm" variant="ghost" loading={act.isPending && act.variables?.id === r.tg_id && !act.variables.ok} onClick={() => act.mutate({ id: r.tg_id, ok: false })}>
                      {t("adminBot.refuse")}
                    </Button>
                  </div>
                </li>
              ))}
            </ul>
          )
        }
      </QueryBoundary>
    </section>
  );
}

function LinkedCard({ v }: { v: AdminBotView }) {
  const qc = useQueryClient();
  const toast = useToast();
  const unlink = useMutation({
    mutationFn: async (id: number) => {
      const res = await raw(`/api/v1/admin-bot/chats/${id}`, { method: "DELETE", headers: { ...csrfHeader() } });
      if (!res.ok && res.status !== 204) throw await toError(res);
    },
    onSuccess: () => void qc.invalidateQueries({ queryKey: VIEW_KEY }),
    onError: (e) => toast.error(failText(e)),
  });
  return (
    <section className="card glass reveal" aria-label={t("adminBot.linked")}>
      <div className="card-head">
        <div>
          <h2 className="card-title">{t("adminBot.linked")}</h2>
          <div className="card-sub">{t("adminBot.linkedSub", { n: v.linked.length })}</div>
        </div>
      </div>
      {v.linked.length === 0 ? (
        <p className="py-4 text-center text-[13px] text-[var(--ink-500)]">{t("adminBot.linkedEmpty")}</p>
      ) : (
        <ul className="row-list">
          {v.linked.map((c) => (
            <li key={c.tg_id} className="flex flex-wrap items-center justify-between gap-3 py-3">
              <div className="min-w-0">
                <div className="truncate text-[13px] font-medium">
                  {c.first_name} {c.username ? <span className="text-[var(--ink-500)]">@{c.username}</span> : null}
                </div>
                <div className="truncate text-xs text-[var(--ink-500)]">
                  {c.admin_username}
                  {c.is_owner ? ` · ${t("admins.owner")}` : ""} · {ago(c.created_at)}
                </div>
              </div>
              <Button size="sm" variant="danger" loading={unlink.isPending && unlink.variables === c.tg_id} onClick={() => unlink.mutate(c.tg_id)}>
                <Trash2 size={16} aria-hidden /> {t("adminBot.unlink")}
              </Button>
            </li>
          ))}
        </ul>
      )}
    </section>
  );
}

function BroadcastCard({ v }: { v: AdminBotView }) {
  const qc = useQueryClient();
  const toast = useToast();
  const [text, setText] = useState("");
  const send = useMutation({
    mutationFn: async () => {
      const res = await raw("/api/v1/admin-bot/broadcast", { method: "POST", headers: { "Content-Type": "application/json", ...csrfHeader() }, body: JSON.stringify({ text }) });
      if (!res.ok) throw await toError(res);
      return (await res.json()) as { queued: number };
    },
    onSuccess: (r) => {
      toast.ok(t("adminBot.broadcastSent", { n: r.queued }));
      setText("");
      void qc.invalidateQueries({ queryKey: VIEW_KEY });
    },
    onError: (e) => toast.error(failText(e)),
  });
  const busy = !!v.broadcast?.active;
  return (
    <section className="card glass reveal">
      <div className="card-head">
        <div>
          <h2 className="card-title">{t("adminBot.broadcast")}</h2>
          <div className="card-sub">{t("adminBot.broadcastSub", { n: v.linked.length })}</div>
        </div>
        <Megaphone size={18} className="text-[var(--ink-400)]" aria-hidden />
      </div>
      <textarea className="input" rows={3} maxLength={3500} value={text} onChange={(e) => setText(e.target.value)} placeholder={t("adminBot.broadcastPlaceholder")} aria-label={t("adminBot.broadcast")} disabled={!v.running} />
      <div className="mt-3 flex flex-wrap items-center gap-3">
        <Button variant="primary" disabled={!v.running || busy || !text.trim()} loading={send.isPending} onClick={() => send.mutate()}>
          <Send size={16} aria-hidden /> {t("adminBot.broadcastButton")}
        </Button>
        <span className="text-xs text-[var(--ink-500)]">{busy ? t("adminBot.broadcasting", { done: v.broadcast!.sent, total: v.broadcast!.total }) : !v.running ? t("adminBot.broadcastOff") : t("adminBot.broadcastHint")}</span>
      </div>
    </section>
  );
}

const ADMIN_BOT_ACTIONS: AdminBotAction[] = ["stats", "find", "nodes", "broadcast", "requests", "unlink", "help", "url", "page"];

function adminActionLabel(a: string): string {
  return tMaybe(`adminBot.menu.actionLabel.${a}`) ?? a;
}

/**
 * The admin bot's menu and texts, mirroring the user bot's Menu&Texts tab: buttons
 * with on/off, labels, row toggles, action select and url/text fields, plus the lang
 * and the two texts. Saved with PATCH {menu}; the view is refetched on success.
 */
function MenuTextsCard({ v }: { v: AdminBotView }) {
  const qc = useQueryClient();
  const toast = useToast();
  const fallback: AdminBotMenu = { lang: "ru", buttons: [], texts: { menu: "", help: "" } };
  const source = v.menu ?? v.menu_defaults ?? fallback;
  const { draft, setDraft, dirty, reset } = useDraft<AdminBotMenu>(source);
  const save = useMutation({
    mutationFn: async () => patchView({ menu: draft }),
    onSuccess: (next) => {
      qc.setQueryData(VIEW_KEY, next);
      toast.ok(t("adminBot.menu.saved"));
      void qc.invalidateQueries({ queryKey: VIEW_KEY });
    },
    onError: (e) => toast.error(failText(e)),
  });
  const set = (i: number, patch: Partial<AdminBotMenuButton>) => setDraft({ ...draft, buttons: draft.buttons.map((b, j) => (j === i ? { ...b, ...patch } : b)) });
  const move = (i: number, d: -1 | 1) => {
    const list = [...draft.buttons];
    [list[i], list[i + d]] = [list[i + d]!, list[i]!];
    setDraft({ ...draft, buttons: list });
  };
  const add = (action: "url" | "page") => {
    setDraft({
      ...draft,
      buttons: [...draft.buttons, { id: `c${Date.now().toString(36)}`, action, label: action === "url" ? t("adminBot.menu.newLink") : t("adminBot.menu.newPage"), on: true, row: false, url: action === "url" ? "https://" : undefined, text: action === "page" ? "" : undefined }],
    });
  };
  const custom = (b: AdminBotMenuButton) => b.action === "url" || b.action === "page";
  if (!v.menu && !v.menu_defaults) return null;
  return (
    <section className="card glass reveal">
      <div className="card-head">
        <div>
          <h2 className="card-title">{t("adminBot.menu.title")}</h2>
          <div className="card-sub">{t("adminBot.menu.sub")}</div>
        </div>
        {v.menu_defaults ? (
          <Button size="sm" variant="ghost" onClick={() => setDraft(v.menu_defaults!)} disabled={save.isPending}>
            {t("adminBot.menu.reset")}
          </Button>
        ) : null}
      </div>
      <Field label={t("adminBot.menu.lang")} hint={t("adminBot.menu.langHint")}>
        <Segmented
          label={t("adminBot.menu.lang")}
          value={draft.lang}
          onChange={(lang) => setDraft({ ...draft, lang })}
          options={[
            { value: "ru", label: "Русский" },
            { value: "en", label: "English" },
          ]}
        />
      </Field>
      <ul className="flex flex-col gap-2">
        {draft.buttons.map((b, i) => (
          <li key={b.id} className="panel-soft p-3">
            <div className="flex items-center gap-2">
              <Switch checked={b.on} label={t("adminBot.menu.showButton", { name: b.label })} onChange={(on) => set(i, { on })} />
              <input className="input h-10 min-w-0 flex-1" value={b.label} maxLength={40} onChange={(e) => set(i, { label: e.target.value })} aria-label={t("adminBot.menu.buttonLabel")} />
              <button type="button" className="icon-btn" disabled={i === 0} onClick={() => move(i, -1)} aria-label={t("adminBot.menu.moveUp")}>
                <ArrowUp size={16} />
              </button>
              <button type="button" className="icon-btn" disabled={i === draft.buttons.length - 1} onClick={() => move(i, 1)} aria-label={t("adminBot.menu.moveDown")}>
                <ArrowDown size={16} />
              </button>
              {custom(b) ? (
                <button type="button" className="icon-btn" onClick={() => setDraft({ ...draft, buttons: draft.buttons.filter((_, j) => j !== i) })} aria-label={t("adminBot.menu.deleteButton", { name: b.label })}>
                  <Trash2 size={16} />
                </button>
              ) : null}
            </div>
            <div className="mt-2 flex flex-wrap items-center gap-2">
              <select className="input h-10 max-w-[240px]" value={b.action} onChange={(e) => set(i, { action: e.target.value as AdminBotAction })} aria-label={t("adminBot.menu.action")}>
                {ADMIN_BOT_ACTIONS.map((a) => (
                  <option key={a} value={a}>
                    {adminActionLabel(a)}
                  </option>
                ))}
              </select>
              {i > 0 ? (
                <label className="flex items-center gap-1.5 text-xs text-[var(--ink-500)]">
                  <input type="checkbox" className="check" checked={b.row} onChange={(e) => set(i, { row: e.target.checked })} /> {t("adminBot.menu.sameRow")}
                </label>
              ) : null}
            </div>
            {b.action === "url" ? <input className="input mono mt-2" value={b.url ?? ""} onChange={(e) => set(i, { url: e.target.value })} placeholder="https://… / tg://…" aria-label={t("adminBot.menu.buttonUrl")} /> : null}
            {b.action === "page" ? (
              <textarea className="input mt-2" value={b.text ?? ""} maxLength={3000} onChange={(e) => set(i, { text: e.target.value })} placeholder={t("adminBot.menu.pagePlaceholder")} aria-label={t("adminBot.menu.pageText")} />
            ) : null}
          </li>
        ))}
      </ul>
      <div className="mt-3 flex flex-wrap gap-2">
        <button type="button" className="chip-btn" onClick={() => add("url")} disabled={draft.buttons.length >= 20}>
          <Plus size={14} className="mr-1 inline" aria-hidden />
          {t("adminBot.menu.addLink")}
        </button>
        <button type="button" className="chip-btn" onClick={() => add("page")} disabled={draft.buttons.length >= 20}>
          <Plus size={14} className="mr-1 inline" aria-hidden />
          {t("adminBot.menu.addPage")}
        </button>
      </div>
      <Field label={t("adminBot.menu.textMenu")} htmlFor="ab-text-menu" hint={t("adminBot.menu.textsHint")}>
        <textarea id="ab-text-menu" className="input" rows={3} maxLength={3000} value={draft.texts.menu} placeholder={v.menu_defaults?.texts.menu ?? ""} onChange={(e) => setDraft({ ...draft, texts: { ...draft.texts, menu: e.target.value } })} />
      </Field>
      <Field label={t("adminBot.menu.textHelp")} htmlFor="ab-text-help" hint={t("adminBot.menu.textsHint")}>
        <textarea id="ab-text-help" className="input" rows={3} maxLength={3000} value={draft.texts.help} placeholder={v.menu_defaults?.texts.help ?? ""} onChange={(e) => setDraft({ ...draft, texts: { ...draft.texts, help: e.target.value } })} />
      </Field>
      <div className="flex flex-wrap gap-2">
        <Button variant="primary" loading={save.isPending} disabled={!dirty} onClick={() => save.mutate()}>
          {t("adminBot.menu.save")}
        </Button>
        {dirty ? (
          <Button variant="ghost" onClick={reset}>
            {t("telegram.discard")}
          </Button>
        ) : null}
      </div>
    </section>
  );
}
