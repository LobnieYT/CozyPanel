import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { useState } from "react";
import { api, errorText, unwrap, type Schemas } from "../../api/client";
import { meQuery, qk } from "../../api/hooks";
import { Confirm, Drawer } from "../../components/overlay";
import { QueryBoundary } from "../../components/query";
import { Switch } from "../../components/switch";
import { useToast } from "../../components/toast";
import { Button, Field, PageHeader, Pill, Skeleton } from "../../components/ui";
import { t } from "../../i18n";
import { dateShort } from "../../lib/format";

type Admin = Schemas["AdminView"];
type Grant = "" | "r" | "w";

const SECTIONS = ["users", "inbounds", "node", "network", "settings", "tariffs", "telegram", "stats", "api-keys"] as const;

const PRESETS: Record<string, Record<string, Grant>> = {
  viewer: { users: "r", inbounds: "r", node: "r", network: "r", settings: "r", tariffs: "r", telegram: "r", stats: "r", "api-keys": "r" },
  operator: { users: "w", inbounds: "w", node: "w", network: "r", settings: "r", tariffs: "r", telegram: "r", stats: "r", "api-keys": "r" },
};

function toUnix(date: string): number | null {
  if (!date) return null;
  const ms = Date.parse(date + "T00:00:00Z");
  return Number.isNaN(ms) ? null : Math.floor(ms / 1000);
}

function toDate(unix?: string | null): string {
  if (!unix) return "";
  const d = new Date(unix);
  return Number.isNaN(d.getTime()) ? "" : d.toISOString().slice(0, 10);
}

export function AdminsPage() {
  const toast = useToast();
  const qc = useQueryClient();
  const me = useQuery(meQuery);
  const admins = useQuery({ queryKey: [...qk.users, "admins"], queryFn: ({ signal }) => unwrap(api.GET("/api/v1/admins", { signal })) });
  if (me.data && !me.data.admin.owner) {
    return (
      <>
        <PageHeader title={t("admins.title")} />
        <p className="text-[13px] text-[var(--berry-600)]" role="alert">
          {t("errors.api.no_grant")}
        </p>
      </>
    );
  }
  const [edit, setEdit] = useState<Admin | "new" | null>(null);
  const [del, setDel] = useState<Admin | null>(null);
  const remove = useMutation({
    mutationFn: (id: number) => unwrap(api.DELETE("/api/v1/admins/{id}", { params: { path: { id } } })),
    onSuccess: () => {
      void qc.invalidateQueries({ queryKey: [...qk.users, "admins"] });
      toast.ok(t("admins.deleted"));
    },
    onError: (e) => toast.error(errorText(e)),
  });
  return (
    <>
      <PageHeader
        title={t("admins.title")}
        sub={t("admins.sub")}
        actions={
          <Button variant="primary" onClick={() => setEdit("new")}>
            {t("admins.create")}
          </Button>
        }
      />
      <QueryBoundary query={admins} pending={<Skeleton style={{ height: 200, borderRadius: 20 }} />}>
        {(list) => (
          <section className="card glass max-w-4xl">
            <ul className="row-list">
              {(list ?? []).map((a) => (
                <li key={a.id} className="flex flex-wrap items-center justify-between gap-3 py-3">
                  <div className="min-w-0">
                    <div className="flex items-center gap-2 text-[13px] font-semibold">
                      <span className="truncate">{a.username}</span>
                      {a.owner ? (
                        <Pill tone="ok">{t("admins.owner")}</Pill>
                      ) : a.disabled ? (
                        <Pill tone="off">{t("admins.disabled")}</Pill>
                      ) : null}
                    </div>
                    <div className="text-xs text-[var(--ink-500)]">
                      {a.expires_at ? t("admins.until", { date: dateShort(a.expires_at) }) : t("admins.forever")}
                      {a.last_login_at ? ` · ${t("admins.lastLogin", { ago: dateShort(a.last_login_at) })}` : ""}
                    </div>
                  </div>
                  {!a.owner ? (
                    <div className="flex gap-1">
                      <Button variant="glass" onClick={() => setEdit(a)}>
                        {t("admins.edit")}
                      </Button>
                      <Button variant="ghost" onClick={() => setDel(a)}>
                        {t("common.delete")}
                      </Button>
                    </div>
                  ) : null}
                </li>
              ))}
            </ul>
          </section>
        )}
      </QueryBoundary>
      {edit ? (
        <AdminDrawer
          admin={edit === "new" ? null : edit}
          onClose={() => setEdit(null)}
          onSaved={() => {
            setEdit(null);
            void qc.invalidateQueries({ queryKey: [...qk.users, "admins"] });
          }}
        />
      ) : null}
      <Confirm
        open={del !== null}
        onOpenChange={(v) => !v && setDel(null)}
        title={t("admins.deleteTitle")}
        text={del?.username ?? ""}
        confirm={t("common.delete")}
        danger
        loading={remove.isPending}
        onConfirm={() => del && remove.mutate(del.id, { onSuccess: () => setDel(null) })}
      />
    </>
  );
}

function AdminDrawer({ admin, onClose, onSaved }: { admin: Admin | null; onClose: () => void; onSaved: () => void }) {
  const toast = useToast();
  const fail = (e: unknown) => toast.error(errorText(e));
  const [username, setUsername] = useState(admin?.username ?? "");
  const [password, setPassword] = useState("");
  const [expires, setExpires] = useState(toDate(admin?.expires_at));
  const [disabled, setDisabled] = useState(admin?.disabled ?? false);
  const [matrix, setMatrix] = useState<Record<string, Grant>>(() => ({ ...(admin?.scopes ?? {}) }) as Record<string, Grant>);
  const [preset, setPreset] = useState("custom");
  const save = useMutation({
    mutationFn: () => {
      const scopes: Record<string, string> = {};
      for (const [k, v] of Object.entries(matrix)) if (v) scopes[k] = v;
      const exp = toUnix(expires);
      if (admin) {
        return unwrap(
          api.PATCH("/api/v1/admins/{id}", {
            params: { path: { id: admin.id } },
            // Empty date on a termed account clears it (0), not leaves it.
            body: { password: password || undefined, disabled, expires_at: exp ?? (admin.expires_at ? 0 : undefined), scopes },
          }),
        );
      }
      return unwrap(api.POST("/api/v1/admins", { body: { username, password, expires_at: exp ?? undefined, scopes } }));
    },
    onSuccess: () => {
      toast.ok(t("settings.saved"));
      onSaved();
    },
    onError: fail,
  });
  const applyPreset = (p: string) => {
    setPreset(p);
    if (p !== "custom") setMatrix({ ...PRESETS[p] });
  };
  const presetLabel = (p: string) => (p === "viewer" ? t("admins.preset_viewer") : p === "operator" ? t("admins.preset_operator") : t("admins.preset_custom"));
  return (
    <Drawer open onOpenChange={(v) => !v && onClose()} title={admin ? admin.username : t("admins.create")}>
      <div className="pt-5">
        {!admin ? (
          <>
            <Field label={t("admins.username")} error={undefined}>
              <input className="input mono" value={username} onChange={(e) => setUsername(e.target.value)} placeholder="moder" spellCheck={false} autoComplete="off" />
            </Field>
            <Field label={t("admins.password")} hint={t("admins.passwordHint")}>
              <input className="input mono" type="password" value={password} onChange={(e) => setPassword(e.target.value)} autoComplete="new-password" />
            </Field>
          </>
        ) : (
          <Field label={t("admins.newPassword")} hint={t("admins.passwordHint")}>
            <input className="input mono" type="password" value={password} onChange={(e) => setPassword(e.target.value)} placeholder="••••••••••••" autoComplete="new-password" />
          </Field>
        )}
        <Field label={t("admins.expires")} hint={t("admins.expiresHint")}>
          <input className="input max-w-[200px]" type="date" value={expires} onChange={(e) => setExpires(e.target.value)} />
        </Field>
        {!admin ? null : (
          <div className="mb-4 flex items-center justify-between gap-3">
            <div className="text-[13px] font-semibold">{t("admins.disabled")}</div>
            <Switch checked={disabled} onChange={setDisabled} label={t("admins.disabled")} />
          </div>
        )}
        <Field label={t("admins.preset")}>
          <div className="flex gap-2">
            {["viewer", "operator", "custom"].map((p) => (
              <button key={p} type="button" className="chip-btn" aria-pressed={preset === p} onClick={() => applyPreset(p)}>
                {presetLabel(p)}
              </button>
            ))}
          </div>
        </Field>
        <div className="mb-1 text-[13px] font-semibold">{t("admins.matrix")}</div>
        <p className="mb-3 text-xs text-[var(--ink-500)]">{t("admins.matrixHint")}</p>
        <ul className="mb-4 flex flex-col gap-2">
          {SECTIONS.map((s) => (
            <li key={s} className="flex items-center justify-between gap-3">
              <span className="text-[13px]">{t(`admins.sec_${s}`)}</span>
              <div className="seg" role="group" aria-label={t(`admins.sec_${s}`)}>
                {(["", "r", "w"] as const).map((v) => (
                  <button
                    key={v}
                    type="button"
                    aria-pressed={(matrix[s] ?? "") === v}
                    onClick={() => {
                      setPreset("custom");
                      setMatrix((m) => ({ ...m, [s]: v }));
                    }}
                  >
                    {v === "" ? "—" : v === "r" ? t("admins.read") : t("admins.write")}
                  </button>
                ))}
              </div>
            </li>
          ))}
        </ul>
        <Button variant="primary" loading={save.isPending} onClick={() => save.mutate()}>
          {t("common.save")}
        </Button>
      </div>
    </Drawer>
  );
}
