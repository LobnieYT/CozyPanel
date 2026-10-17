import { Ban, Laptop, Layers, Pencil, Smartphone, Unlink } from "lucide-react";
import { useState } from "react";
import { ApiError, errorText, type Schemas, type User } from "../../../api/client";
import { useBoundDevices, useDeviceBans, useDevices, useGrants, userActions, useSettings, useUserMutation } from "../../../api/hooks";
import { Confirm } from "../../../components/overlay";
import { DeviceModePicker, modeOf } from "./device-mode";import { useToast } from "../../../components/toast";
import { ErrorState, Skeleton } from "../../../components/ui";
import { t } from "../../../i18n";
import { desktopOS, deviceDetails, deviceLabel } from "../../../lib/devices";
import { ago, bytes, dateShort, maskIP } from "../../../lib/format";
import { Section } from "./section";

export function DevicesSection({ u }: { u: User }) {
  const devices = useDevices(u.id);
  const toast = useToast();
  const update = useUserMutation(userActions.update);
  const { can } = useGrants();
  const writable = can("users", true);
  const list = devices.data ?? [];
  const [revealed, setRevealed] = useState<ReadonlySet<string>>(new Set());
  const [showAll, setShowAll] = useState(false);
  const visible = showAll ? list : list.slice(0, 8);
  const toggleIP = (ip: string) =>
    setRevealed((prev) => {
      const next = new Set(prev);
      if (next.has(ip)) next.delete(ip);
      else next.add(ip);
      return next;
    });
  const setLimit = (n: number | null) =>
    update.mutate({ id: u.id, body: n === null ? { devices_unlimited: true } : { device_limit: n } }, { onError: (e) => toast.error(errorText(e)) });
  return (
    <Section title={t("userDrawer.devices")} aside={t("userDrawer.devicesAside", { online: u.online_ips.length, limit: u.device_limit ?? "∞" })}>
      <div className="mb-4 flex items-center gap-2 text-[13px]">
        <span className="text-[var(--ink-600)]">{t("userDrawer.deviceLimit")}</span>
        {writable ? (
        <div className="seg" role="group" aria-label={t("userDrawer.deviceLimit")}>
          {[1, 2, 3, 5, 10].map((n) => (
            <button key={n} type="button" aria-pressed={u.device_limit === n} onClick={() => setLimit(n)}>
              {n}
            </button>
          ))}
          <button type="button" aria-pressed={u.device_limit == null} onClick={() => setLimit(null)}>
            ∞
          </button>
        </div>
        ) : (
          <b className="num">{u.device_limit ?? "∞"}</b>
        )}
      </div>
      <BoundDevices u={u} />
      <div className="mt-4 flex items-center gap-2 text-[13px]">
        <span className="text-[var(--ink-600)]">{t("deviceMode.label")}</span>
        <DeviceModePicker
          value={modeOf(u)}
          onChange={(m) => update.mutate({ id: u.id, body: { device_mode: m } }, { onError: (e) => toast.error(errorText(e)) })}
        />
      </div>
      <h4 className="mt-5 mb-2 text-xs font-medium text-[var(--ink-500)]">{t("userDrawer.addresses")}</h4>
      {devices.isPending ? (
        <Skeleton style={{ height: 52, borderRadius: 16 }} />
      ) : devices.data === undefined ? (
        <ErrorState text={errorText(devices.error)} onRetry={() => void devices.refetch()} />
      ) : list.length === 0 ? (
        <p className="text-[13px] text-[var(--ink-500)]">{t("userDrawer.noDevices")}</p>
      ) : (
        <ul className="flex flex-col gap-2">
          {visible.map((d) => (
            <li key={d.ip} className="panel-soft grid grid-cols-[36px_minmax(0,1fr)] items-center gap-3 p-2">
              <span className="grid h-9 w-9 place-items-center rounded-[10px] bg-[var(--hover)] text-[var(--ink-600)]"><Smartphone size={18} /></span>
              <div className="min-w-0">
                <div className="text-[13px] font-medium">{t("userDrawer.device")}</div>
                <div className="text-xs text-[var(--ink-500)]">
                  <button
                    type="button"
                    className="mono underline decoration-dotted underline-offset-2"
                    title={t(revealed.has(d.ip) ? "userDrawer.hideIP" : "userDrawer.showIP")}
                    onClick={() => toggleIP(d.ip)}
                  >
                    {revealed.has(d.ip) ? d.ip : maskIP(d.ip)}
                  </button>{" "}
                  · {d.online ? <span className="text-[var(--leaf-700)]">{t("users.onlineNow")}</span> : ago(d.last_seen)}
                </div>
              </div>
            </li>
          ))}
        </ul>
      )}
      {list.length > 8 ? (
        <button type="button" className="mt-2 text-[13px] font-medium text-[var(--cozy-400)]" onClick={() => setShowAll((v) => !v)}>
          {showAll ? t("userDrawer.showLess") : t("userDrawer.showAll", { n: list.length })}
        </button>
      ) : null}
      <p className="mt-2 text-xs text-[var(--ink-500)]">{t("userDrawer.devicesNote")}</p>
    </Section>
  );
}

type BoundDevice = Schemas["BoundDeviceView"];
type DeviceBan = Schemas["DeviceBanView"];

const DEVICE_NAME_MAX = 40;

/** What to call a bound device: its own name, else the app's report. */
function deviceName(d: BoundDevice): string {
  if (d.name) return d.name;
  if (!d.hwid) return t("userDrawer.sharedPlace");
  return deviceLabel(d, t("userDrawer.device"));
}

/** The name the app reports: the rename field's placeholder. */
function reportedName(d: BoundDevice): string {
  if (!d.hwid) return t("userDrawer.sharedPlace");
  return deviceLabel(d, t("userDrawer.device"));
}

type Pick = { kind: "unbind" | "ban" | "rename"; d: BoundDevice } | { kind: "unban"; ban: DeviceBan };

function BoundDevices({ u }: { u: User }) {
  const settings = useSettings();
  const bound = useBoundDevices(u.id);
  const unbind = useUserMutation(userActions.unbindDevice);
  const rename = useUserMutation(userActions.renameDevice);
  const ban = useUserMutation(userActions.banDevice);
  const unban = useUserMutation(userActions.unbanDevice);
  const { can } = useGrants();
  const writable = can("users", true);
  const toast = useToast();
  const [pick, setPick] = useState<Pick | null>(null);
  const [name, setName] = useState("");
  const [nameError, setNameError] = useState("");
  const list = bound.data ?? [];
  if (!settings.data?.device_binding && list.length === 0) return null;
  const startRename = (d: BoundDevice) => {
    setName(d.name);
    setNameError("");
    setPick({ kind: "rename", d });
  };
  const doRename = () => {
    if (pick?.kind !== "rename") return;
    const clean = name.trim();
    if (clean === pick.d.name) {
      setPick(null);
      return;
    }
    setNameError("");
    rename.mutate(
      { id: u.id, device: pick.d.id, name: clean },
      {
        onSuccess: (saved) => {
          toast.ok(t(clean ? "userDrawer.deviceRenamed" : "userDrawer.deviceNameCleared", { name: saved.name || reportedName(pick.d) }));
          setPick(null);
        },
        onError: (e) => {
          if (e instanceof ApiError && e.fields.name) setNameError(e.fields.name);
          else toast.error(errorText(e));
        },
      },
    );
  };
  const unpick = pick?.kind === "unbind" ? pick.d : null;
  return (
    <>
      <h4 className="mb-2 flex justify-between gap-2 text-xs font-medium text-[var(--ink-500)]">
        {t("userDrawer.boundTitle")}
        {bound.data ? <span className="num">{u.device_limit != null ? t("userDrawer.boundCount", { n: list.length, limit: u.device_limit }) : list.length}</span> : null}
      </h4>
      {bound.data === undefined && !bound.isError ? (
        <Skeleton style={{ height: 52, borderRadius: 16 }} />
      ) : bound.data === undefined ? (
        <ErrorState text={errorText(bound.error)} onRetry={() => void bound.refetch()} />
      ) : list.length === 0 ? (
        <p className="text-[13px] text-[var(--ink-500)]">{t("userDrawer.boundEmpty")}</p>
      ) : (
        <ul className="flex flex-col gap-2">
          {list.map((d) => {
            const meta = deviceDetails(d, !!d.hwid);
            return (
              <li key={d.id} className="panel-soft grid grid-cols-[36px_minmax(0,1fr)_auto] items-center gap-3 p-2">
                <span className="grid h-9 w-9 place-items-center rounded-[10px] bg-[var(--hover)] text-[var(--ink-600)]" aria-hidden>
                  {!d.hwid ? <Layers size={18} /> : desktopOS.test(d.os) ? <Laptop size={18} /> : <Smartphone size={18} />}
                </span>
                <div className="min-w-0">
                  <div className="truncate text-[13px] font-medium">{deviceName(d)}</div>
                  <div className="truncate text-xs text-[var(--ink-500)]">
                    {meta ? `${meta} · ` : ""}
                    {d.online ? <span className="text-[var(--leaf-700)]">{t("users.onlineNow")}</span> : ago(d.last_seen)}
                  </div>
                  {d.traffic_up + d.traffic_down > 0 ? (
                    <div className="num text-xs text-[var(--ink-500)]" title={t("userDrawer.deviceTrafficHint")}>
                      {t("userDrawer.deviceTraffic", { down: bytes(d.traffic_down), up: bytes(d.traffic_up) })}
                    </div>
                  ) : null}
                </div>
                {writable ? (
                  <div className="flex gap-1">
                    <button type="button" className="icon-btn" aria-label={t("userDrawer.renameDeviceLabel", { name: deviceName(d) })} title={t("userDrawer.renameDevice")} onClick={() => startRename(d)}>
                      <Pencil size={16} />
                    </button>
                    {d.hwid ? (
                      <button type="button" className="icon-btn" aria-label={t("userDrawer.banLabel", { name: deviceName(d) })} title={t("userDrawer.ban")} onClick={() => setPick({ kind: "ban", d })}>
                        <Ban size={16} />
                      </button>
                    ) : null}
                    <button type="button" className="icon-btn" aria-label={t("userDrawer.unbindLabel", { name: deviceName(d) })} title={t("userDrawer.unbind")} onClick={() => setPick({ kind: "unbind", d })}>
                      <Unlink size={16} />
                    </button>
                  </div>
                ) : null}
              </li>
            );
          })}
        </ul>
      )}
      <DeviceBans u={u} onUnban={(ban) => setPick({ kind: "unban", ban })} />
      <p className="mt-2 text-xs text-[var(--ink-500)]">{settings.data?.device_require_hwid ? t("userDrawer.boundNoteStrict") : t("userDrawer.boundNote")}</p>
      <Confirm
        open={unpick !== null}
        onOpenChange={(v) => !v && setPick(null)}
        title={t("userDrawer.unbindTitle", { name: unpick ? deviceName(unpick) : "" })}
        text={unpick && !unpick.hwid ? t("userDrawer.unbindSharedText") : t("userDrawer.unbindText")}
        confirm={t("userDrawer.unbind")}
        danger
        loading={unbind.isPending}
        onConfirm={() =>
          unpick &&
          unbind.mutate(
            { id: u.id, device: unpick.id },
            {
              onSuccess: () => {
                toast.ok(t("userDrawer.unbound", { name: deviceName(unpick) }));
                setPick(null);
              },
              onError: (e) => toast.error(errorText(e)),
            },
          )
        }
      />
      <Confirm
        open={pick?.kind === "ban"}
        onOpenChange={(v) => !v && setPick(null)}
        title={t("userDrawer.banTitle", { name: pick?.kind === "ban" ? deviceName(pick.d) : "" })}
        text={t("userDrawer.banText")}
        confirm={t("userDrawer.ban")}
        danger
        loading={ban.isPending}
        onConfirm={() =>
          pick?.kind === "ban" &&
          ban.mutate(
            { id: u.id, device: pick.d.id },
            {
              onSuccess: () => {
                toast.ok(t("userDrawer.banned", { name: deviceName(pick.d) }));
                setPick(null);
              },
              onError: (e) => toast.error(errorText(e)),
            },
          )
        }
      />
      <Confirm
        open={pick?.kind === "unban"}
        onOpenChange={(v) => !v && setPick(null)}
        title={t("userDrawer.unbanTitle", { name: pick?.kind === "unban" ? pick.ban.label || pick.ban.hwid : "" })}
        text={t("userDrawer.unbanText")}
        confirm={t("userDrawer.unban")}
        loading={unban.isPending}
        onConfirm={() =>
          pick?.kind === "unban" &&
          unban.mutate(
            { id: u.id, ban: pick.ban.id },
            {
              onSuccess: () => {
                toast.ok(t("userDrawer.unbanned", { name: pick.ban.label || pick.ban.hwid }));
                setPick(null);
              },
              onError: (e) => toast.error(errorText(e)),
            },
          )
        }
      />
      <Confirm
        open={pick?.kind === "rename"}
        onOpenChange={(v) => !v && setPick(null)}
        title={t("userDrawer.renameDeviceTitle", { name: pick?.kind === "rename" ? reportedName(pick.d) : "" })}
        text={
          <>
            <p className="mb-2">{t("userDrawer.renameDeviceHint")}</p>
            <input
              className="input"
              value={name}
              maxLength={DEVICE_NAME_MAX}
              placeholder={pick?.kind === "rename" ? reportedName(pick.d) : ""}
              aria-label={t("userDrawer.renameDeviceField")}
              autoFocus
              onChange={(e) => setName(e.target.value)}
              onKeyDown={(e) => {
                if (e.key === "Enter") doRename();
              }}
            />
            {nameError ? <p className="mt-1 text-[13px] text-[var(--berry-600)]">{nameError}</p> : null}
          </>
        }
        confirm={t("userDrawer.renameDevice")}
        loading={rename.isPending}
        onConfirm={doRename}
      />
    </>
  );
}

/** Device ids blocked from the subscription, each with its unblock button. */
function DeviceBans({ u, onUnban }: { u: User; onUnban: (ban: DeviceBan) => void }) {
  const bans = useDeviceBans(u.id);
  const { can } = useGrants();
  const writable = can("users", true);
  const list = bans.data ?? [];
  if (list.length === 0) return null;
  return (
    <>
      <h4 className="mt-4 mb-2 text-xs font-medium text-[var(--ink-500)]">{t("userDrawer.bansTitle")}</h4>
      <ul className="flex flex-col gap-2">
        {list.map((b) => (
          <li key={b.id} className="panel-soft grid grid-cols-[36px_minmax(0,1fr)_auto] items-center gap-3 p-2">
            <span className="grid h-9 w-9 place-items-center rounded-[10px] bg-[var(--hover)] text-[var(--berry-600)]" aria-hidden>
              <Ban size={18} />
            </span>
            <div className="min-w-0">
              <div className="truncate text-[13px] font-medium">{b.label || b.hwid}</div>
              <div className="truncate text-xs text-[var(--ink-500)]">
                {b.admin ? `${b.admin} · ` : ""}
                {dateShort(b.banned_at)}
              </div>
            </div>
            {writable ? (
              <button type="button" className="icon-btn" aria-label={t("userDrawer.unbanLabel", { name: b.label || b.hwid })} title={t("userDrawer.unban")} onClick={() => onUnban(b)}>
                <Unlink size={16} />
              </button>
            ) : null}
          </li>
        ))}
      </ul>
    </>
  );
}
