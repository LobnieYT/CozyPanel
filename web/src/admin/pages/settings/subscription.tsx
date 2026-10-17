import { useEffect, useState, type FormEvent } from "react";
import type { Schemas } from "../../../api/client";
import { useGrants, useInbounds, useNodes } from "../../../api/hooks";
import { Button, Field, Pill } from "../../../components/ui";
import { Switch } from "../../../components/switch";
import { t } from "../../../i18n";
import { useDraft } from "../../../lib/draft";
import { fieldErrors } from "../../../lib/fields";
import { FingerprintSelect } from "../../../components/fingerprint-select";
import { useSaveSettings } from "./shared";

// Ports the installer opens in the firewall (443 and the HTTPS pool): a subscription
// port among them needs nothing else on the server.
const OPEN_PORTS = [443, 2053, 2083, 2087, 2096, 8443] as const;

// Subscriptions on a port of their own: a usual HTTPS port looks like any site, and the
// admin panel's port stops showing in every link. Links on the old port keep working.
export function SubPortCard({ s }: { s: Schemas["SettingsView"] }) {
  const save = useSaveSettings();
  const { can } = useGrants();
  const writable = can("settings", true);
  const inbounds = useInbounds();
  const nodes = useNodes();
  const { draft: port, setDraft: setPort } = useDraft(s.sub_port ? String(s.sub_port) : "");
  const error = fieldErrors(save.error).sub_port;
  const own = nodes.data?.find((n) => n.local)?.id;
  // Who holds a port over TCP on the panel's own server.
  const holder = (p: number) => (inbounds.data ?? []).find((i) => i.node_id === own && i.enabled && i.network === "tcp" && i.port === String(p))?.name;
  const value = Number(port);
  const valid = port.trim() === "" || (Number.isInteger(value) && value >= 1 && value <= 65535);
  const next = port.trim() === "" ? 0 : value;
  const changed = next !== (s.sub_port ?? 0);
  const submit = (e: FormEvent) => {
    e.preventDefault();
    if (valid) save.mutate({ sub_port: next });
  };
  const current = s.sub_port || s.panel_port;
  return (
    <section className="card glass reveal" style={{ "--i": 1 } as React.CSSProperties}>
      <form onSubmit={submit} noValidate>
        <div className="card-head">
          <div>
            <h2 className="card-title">{t("settings.subPort")}</h2>
            <div className="card-sub">{t("settings.subPortSub")}</div>
          </div>
          {s.sub_port ? <Pill tone={s.sub_port_error ? "bad" : "ok"}>{s.sub_port_error ? t("settings.subPortDown") : t("settings.subPortOn", { port: s.sub_port })}</Pill> : null}
        </div>
        {s.sub_port_error ? (
          <div className="banner err mb-4" role="alert">
            {t("settings.subPortBusy", { port: s.sub_port, panel: s.panel_port })}
          </div>
        ) : null}
        {s.sub_base_url ? (
          <div className="panel-soft mb-4 p-3">
            <div className="text-xs text-[var(--ink-500)]">{t("settings.subPortLinks")}</div>
            <div className="mono truncate text-[13px]">{s.sub_base_url.replace(/[^/]+\/$/, "…")}</div>
          </div>
        ) : null}
        <Field label={t("settings.subPortField")} htmlFor="s-sub-port" hint={t("settings.subPortHint", { panel: s.panel_port })} error={error ?? (valid ? undefined : t("settings.subPortInvalid"))}>
          <input
            id="s-sub-port"
            className="input mono max-w-[140px]"
            inputMode="numeric"
            value={port}
            onChange={(e) => setPort(e.target.value.replace(/[^0-9]/g, ""))}
            placeholder={String(s.panel_port)}
            aria-invalid={!!error || !valid}
          />
        </Field>
        <div className="mb-4 flex flex-wrap gap-2" role="group" aria-label={t("settings.subPortQuick")}>
          {OPEN_PORTS.map((p) => {
            const who = p === s.panel_port ? t("settings.subPortPanel") : holder(p);
            return (
              <button
                key={p}
                type="button"
                className="chip"
                aria-pressed={port === String(p)}
                disabled={!!who}
                title={p === s.panel_port ? t("errors.api.sub_port_panel") : who ? t("settings.subPortHeld", { name: who }) : undefined}
                onClick={() => setPort(String(p))}
              >
                <span className="mono">{p}</span>
                {who ? <span className="text-[var(--ink-400)]"> · {who}</span> : null}
              </button>
            );
          })}
        </div>
        <p className="mb-4 text-xs text-[var(--ink-500)]">{t("settings.subPortNote")}</p>
        <div className="flex flex-wrap gap-2">
          {writable ? (
            <>
              <Button type="submit" variant="primary" loading={save.isPending && save.variables?.sub_port === next} disabled={!changed || !valid}>
                {t("common.save")}
              </Button>
              {s.sub_port ? (
                <Button variant="ghost" loading={save.isPending && save.variables?.sub_port === 0} onClick={() => save.mutate({ sub_port: 0 })}>
                  {t("settings.subPortOff", { port: current === s.sub_port ? s.panel_port : current })}
                </Button>
              ) : null}
            </>
          ) : null}
        </div>
      </form>
    </section>
  );
}

const routingModes = [
  { id: "ru_direct", title: "settings.routingRuDirect", sub: "settings.routingRuDirectSub" },
  { id: "all", title: "settings.routingAll", sub: "settings.routingAllSub" },
] as const;

export function SubscriptionCard({ s }: { s: Schemas["SettingsView"] }) {
  const save = useSaveSettings();
  const { can } = useGrants();
  const writable = can("settings", true);
  const inbounds = useInbounds();
  const { draft: form, setDraft: setForm } = useDraft({ brand: s.brand, support_url: s.support_url, pay_contact: s.pay_contact, sub_group_main: s.sub_group_main, sub_group_auto: s.sub_group_auto, sub_routing: s.sub_routing, client_fingerprint: s.client_fingerprint });
  const [fpOk, setFpOk] = useState(true);
  const errors = fieldErrors(save.error);
  const submit = (e: FormEvent) => {
    e.preventDefault();
    save.mutate({ brand: form.brand, support_url: form.support_url, pay_contact: form.pay_contact, sub_group_main: form.sub_group_main.trim(), sub_group_auto: form.sub_group_auto.trim(), sub_routing: form.sub_routing, client_fingerprint: form.client_fingerprint });
  };
  const set = (k: keyof typeof form) => (e: React.ChangeEvent<HTMLInputElement>) => setForm((f) => ({ ...f, [k]: e.target.value }));
  const proxies = (inbounds.data ?? []).filter((i) => i.enabled).map((i) => i.sub_name);
  return (
    <section className="card glass reveal" style={{ "--i": 1 } as React.CSSProperties}>
      <form onSubmit={submit} noValidate>
        <div className="card-head">
          <div>
            <h2 className="card-title">{t("settings.subscription")}</h2>
            <div className="card-sub">{t("settings.subscriptionSub")}</div>
          </div>
        </div>
        <Field label={t("settings.brand")} htmlFor="s-brand" hint={t("settings.brandHint")}>
          <input id="s-brand" className="input" value={form.brand} onChange={set("brand")} maxLength={40} />
        </Field>
        <div className="grid gap-x-3 sm:grid-cols-2">
          <Field label={t("settings.groupMain")} htmlFor="s-gmain" hint={t("settings.groupMainHint")} error={errors.sub_group_main}>
            <input id="s-gmain" className="input" value={form.sub_group_main} onChange={set("sub_group_main")} maxLength={48} aria-invalid={!!errors.sub_group_main} autoComplete="off" />
          </Field>
          <Field label={t("settings.groupAuto")} htmlFor="s-gauto" hint={t("settings.groupAutoHint")} error={errors.sub_group_auto}>
            <input id="s-gauto" className="input" value={form.sub_group_auto} onChange={set("sub_group_auto")} maxLength={48} aria-invalid={!!errors.sub_group_auto} autoComplete="off" />
          </Field>
        </div>
        <div className="panel-soft mb-4 p-3" aria-label={t("settings.preview")}>
          <div className="mb-2 text-xs text-[var(--ink-500)]">{t("settings.previewHint")}</div>
          <div className="flex items-center gap-2">
            <b className="truncate text-[13px]">{form.sub_group_main || "—"}</b>
            <span className="rounded-md border border-[var(--hairline)] px-1.5 py-0.5 text-[10px] font-semibold tracking-wide text-[var(--ink-500)]">SELECTOR</span>
          </div>
          <div className="mt-2 flex flex-wrap gap-1.5">
            <span className="tag inline-flex items-center gap-1">
              {form.sub_group_auto || "—"}
              <span className="text-[10px] font-semibold tracking-wide text-[var(--ink-400)]">URLTEST</span>
            </span>
            {proxies.map((p) => (
              <span key={p} className="tag">
                {p}
              </span>
            ))}
          </div>
        </div>
        <Field label={t("settings.routing")} hint={t("settings.routingHint")}>
          <div className="grid gap-2 sm:grid-cols-2" role="radiogroup" aria-label={t("settings.routing")}>
            {routingModes.map((m) => (
              <button key={m.id} type="button" role="radio" aria-checked={form.sub_routing === m.id} className="opt" onClick={() => setForm((f) => ({ ...f, sub_routing: m.id }))}>
                <span className="font-semibold">{t(m.title)}</span>
                <span className="text-xs text-[var(--ink-500)]">{t(m.sub)}</span>
              </button>
            ))}
          </div>
        </Field>
        <Field label={t("settings.fingerprint")} htmlFor="s-fp" hint={t("settings.fingerprintHint")} error={errors.client_fingerprint}>
          <FingerprintSelect key={s.client_fingerprint} id="s-fp" value={form.client_fingerprint} onChange={(v) => setForm((f) => ({ ...f, client_fingerprint: v }))} invalid={!!errors.client_fingerprint} onValid={setFpOk} />
        </Field>
        <Field label={t("settings.support")} htmlFor="s-support" hint={t("settings.supportHint")} error={errors.support_url}>
          <input id="s-support" className="input" value={form.support_url} onChange={set("support_url")} placeholder="https://t.me/your_support" aria-invalid={!!errors.support_url} />
        </Field>
        <Field label={t("settings.payContact")} htmlFor="s-pay-contact" hint={t("settings.payContactHint")} error={errors.pay_contact}>
          <input id="s-pay-contact" className="input" value={form.pay_contact} onChange={set("pay_contact")} placeholder="@your_payment_contact" maxLength={64} aria-invalid={!!errors.pay_contact} />
        </Field>
        {writable ? (
          <Button type="submit" variant="primary" loading={save.isPending} disabled={!fpOk || !form.client_fingerprint}>
            {t("common.save")}
          </Button>
        ) : null}
      </form>
    </section>
  );
}

export function DevicesCard({ s }: { s: Schemas["SettingsView"] }) {
  const save = useSaveSettings();
  const { can } = useGrants();
  const writable = can("settings", true);
  return (
    <section className="card glass reveal" style={{ "--i": 4 } as React.CSSProperties}>
      <div className="card-head">
        <div>
          <h2 className="card-title">{t("settings.devices")}</h2>
          <div className="card-sub">{t("settings.devicesSub")}</div>
        </div>
      </div>
      <ul className="row-list">
        <li className="flex items-start justify-between gap-4 py-3">
          <div className="min-w-0">
            <div className="text-[13px] font-medium">{t("settings.binding")}</div>
            <div className="mt-1 text-xs text-[var(--ink-500)]">{t("settings.bindingSub")}</div>
          </div>
          <Switch checked={s.device_binding} label={t("settings.binding")} disabled={save.isPending || !writable} onChange={(v) => writable && save.mutate({ device_binding: v })} />
        </li>
        <li className="flex items-start justify-between gap-4 py-3">
          <div className="min-w-0">
            <div className="text-[13px] font-medium">{t("settings.requireHwid")}</div>
            <div className="mt-1 text-xs text-[var(--ink-500)]">{t("settings.requireHwidSub")}</div>
          </div>
          <Switch checked={s.device_require_hwid} label={t("settings.requireHwid")} disabled={save.isPending || !s.device_binding || !writable} onChange={(v) => writable && save.mutate({ device_require_hwid: v })} />
        </li>
      </ul>
      <UnbindRulesForm current={s.device_unbind} />
      <ReissueRulesForm current={s.link_reissue} />
      <p className="mt-3 text-xs text-[var(--ink-500)]">{t("settings.devicesNote")}</p>
    </section>
  );
}

type UnbindRules = Schemas["UnbindRules"];

/** The subscriber's own unbinds: how many per period, and how soon an unbound device may bind again. */
function UnbindRulesForm({ current }: { current: UnbindRules }) {
  const save = useSaveSettings();
  const { can } = useGrants();
  const writable = can("settings", true);
  const [off, setOff] = useState(current.off ?? false);
  const [limit, setLimit] = useState(String(current.limit ?? 1));
  const [days, setDays] = useState(String(current.days ?? 1));
  const [ret, setRet] = useState(current.return_hours === -1 ? "admin" : current.return_hours === 0 ? "now" : "hours");
  const [hours, setHours] = useState(String(current.return_hours > 0 ? current.return_hours : 24));
  const key = JSON.stringify(current);
  useEffect(() => {
    setOff(current.off ?? false);
    setLimit(String(current.limit ?? 1));
    setDays(String(current.days ?? 1));
    setRet(current.return_hours === -1 ? "admin" : current.return_hours === 0 ? "now" : "hours");
    setHours(String(current.return_hours > 0 ? current.return_hours : 24));
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [key]);
  const errs = fieldErrors(save.error);
  const submit = (e: FormEvent) => {
    e.preventDefault();
    const body = {
      off,
      limit: Number(limit),
      days: Number(days),
      return_hours: ret === "admin" ? -1 : ret === "now" ? 0 : Number(hours),
    };
    save.mutate({ device_unbind: body });
  };
  const summary = off
    ? t("settings.unbindSummaryOff")
    : Number(limit) === 0
      ? t("settings.unbindSummaryFree")
      : t("settings.unbindSummary", { n: Number(limit) || 0, period: periodLabel(Number(days) || 0) }) +
        " " +
        (ret === "admin" ? t("settings.unbindNever") : ret === "now" ? t("settings.unbindAtOnce") : t("settings.unbindSummaryPause", { span: pauseLabel(Number(hours) || 0) }));
  return (
    <form onSubmit={submit} noValidate className="mt-4 border-t border-[var(--hairline)] pt-4">
      <div className="mb-2 flex items-start justify-between gap-4">
        <div className="min-w-0">
          <div className="text-[13px] font-medium">{t("settings.unbindTitle")}</div>
          <div className="mt-1 text-xs text-[var(--ink-500)]">{t("settings.unbindSub")}</div>
        </div>
        <Switch checked={!off} label={t("settings.unbindTitle")} disabled={save.isPending || !writable} onChange={(v) => writable && setOff(!v)} />
      </div>
      {!off ? (
        <>
          <div className="grid grid-cols-2 gap-3">
            <Field label={t("settings.unbindLimit")} htmlFor="s-unbind-limit" error={errs["device_unbind.limit"]}>
              <input id="s-unbind-limit" className="input" inputMode="numeric" value={limit} disabled={!writable} onChange={(e) => setLimit(e.target.value.replace(/[^0-9]/g, ""))} />
            </Field>
            <Field label={t("settings.unbindPeriod")} htmlFor="s-unbind-days" error={errs["device_unbind.days"]}>
              <input id="s-unbind-days" className="input" inputMode="numeric" value={days} disabled={!writable} onChange={(e) => setDays(e.target.value.replace(/[^0-9]/g, ""))} />
            </Field>
          </div>
          <Field label={t("settings.unbindReturn")} error={errs["device_unbind.return_hours"]}>
            <div className="seg" role="group" aria-label={t("settings.unbindReturn")}>
              {(
                [
                  ["now", t("settings.unbindAtOnce")],
                  ["hours", t("settings.unbindHours")],
                  ["admin", t("settings.unbindNever")],
                ] as const
              ).map(([v, label]) => (
                <button key={v} type="button" aria-pressed={ret === v} disabled={!writable} onClick={() => setRet(v)}>
                  {label}
                </button>
              ))}
            </div>
          </Field>
          {ret === "hours" ? (
            <Field label={t("settings.unbindHours")} htmlFor="s-unbind-hours" error={errs["device_unbind.return_hours"]}>
              <input id="s-unbind-hours" className="input max-w-[100px]" inputMode="numeric" value={hours} disabled={!writable} onChange={(e) => setHours(e.target.value.replace(/[^0-9]/g, ""))} />
            </Field>
          ) : null}
          <p className="mb-3 text-xs text-[var(--ink-500)]">{summary}</p>
          <Button variant="primary" type="submit" loading={save.isPending} disabled={!writable}>
            {t("common.save")}
          </Button>
        </>
      ) : (
        <p className="text-xs text-[var(--ink-500)]">{t("settings.unbindOffNote")}</p>
      )}
    </form>
  );
}

function periodLabel(days: number): string {
  if (days === 1) return t("settings.unbindDay");
  if (days === 7) return t("settings.unbindWeek");
  if (days === 30 || days === 31) return t("settings.unbindMonth");
  return t("settings.unbindDays", { n: days });
}

function pauseLabel(hours: number): string {
  if (hours < 24) return t("settings.unbindHoursUnit", { n: hours });
  return periodLabel(Math.ceil(hours / 24));
}

/** New links the subscriber issues on their own: a limit per period, or off. */
function ReissueRulesForm({ current }: { current: Schemas["ReissueRules"] }) {
  const save = useSaveSettings();
  const { can } = useGrants();
  const writable = can("settings", true);
  const [off, setOff] = useState(current.off ?? false);
  const [limit, setLimit] = useState(String(current.limit ?? 1));
  const [days, setDays] = useState(String(current.days ?? 1));
  const key = JSON.stringify(current);
  useEffect(() => {
    setOff(current.off ?? false);
    setLimit(String(current.limit ?? 1));
    setDays(String(current.days ?? 1));
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [key]);
  const errs = fieldErrors(save.error);
  const submit = (e: FormEvent) => {
    e.preventDefault();
    save.mutate({ link_reissue: { off, limit: Number(limit), days: Number(days) } });
  };
  const summary = off
    ? t("settings.reissueSummaryOff")
    : Number(limit) === 0
      ? t("settings.reissueSummaryFree")
      : t("settings.reissueSummary", { n: Number(limit) || 0, period: periodLabel(Number(days) || 0) });
  return (
    <form onSubmit={submit} noValidate className="mt-4 border-t border-[var(--hairline)] pt-4">
      <div className="mb-2 flex items-start justify-between gap-4">
        <div className="min-w-0">
          <div className="text-[13px] font-medium">{t("settings.reissueTitle")}</div>
          <div className="mt-1 text-xs text-[var(--ink-500)]">{t("settings.reissueSub")}</div>
        </div>
        <Switch checked={!off} label={t("settings.reissueTitle")} disabled={save.isPending || !writable} onChange={(v) => writable && setOff(!v)} />
      </div>
      {!off ? (
        <>
          <div className="grid grid-cols-2 gap-3">
            <Field label={t("settings.reissueLimit")} htmlFor="s-reissue-limit" error={errs["link_reissue.limit"]}>
              <input id="s-reissue-limit" className="input" inputMode="numeric" value={limit} disabled={!writable} onChange={(e) => setLimit(e.target.value.replace(/[^0-9]/g, ""))} />
            </Field>
            <Field label={t("settings.unbindPeriod")} htmlFor="s-reissue-days" error={errs["link_reissue.days"]}>
              <input id="s-reissue-days" className="input" inputMode="numeric" value={days} disabled={!writable} onChange={(e) => setDays(e.target.value.replace(/[^0-9]/g, ""))} />
            </Field>
          </div>
          <p className="mb-3 text-xs text-[var(--ink-500)]" role="status">
            {summary}
          </p>
          <Button variant="primary" type="submit" loading={save.isPending} disabled={!writable}>
            {t("common.save")}
          </Button>
        </>
      ) : (
        <p className="text-xs text-[var(--ink-500)]">{t("settings.unbindOffNote")}</p>
      )}
    </form>
  );
}
