import * as Menu from "@radix-ui/react-dropdown-menu";
import { CalendarPlus, Camera, MoreHorizontal, Power, RefreshCw, RotateCcw, Trash2 } from "lucide-react";
import { useRef, useState } from "react";
import { useQueryClient } from "@tanstack/react-query";
import { basePath, csrf, errorText, type User } from "../../api/client";
import { onePeriod, qk, userActions, useGrants, useUser, useUserMutation } from "../../api/hooks";
import { Confirm, Drawer } from "../../components/overlay";
import { useToast } from "../../components/toast";
import { QueryBoundary } from "../../components/query";
import { Avatar, Button, Skeleton, StatePill } from "../../components/ui";
import { t, tMaybe } from "../../i18n";
import { ago, dateShort } from "../../lib/format";
import { GrantsSection } from "./user/grants";
import { PoolsSection, TariffSection, TrafficSection } from "./user/traffic";
import { ExpirySection } from "./user/expiry";
import { NoteSection, ProtocolsSection, KeysSection, SubscriptionSection, TelegramSection } from "./user/access";
import { DevicesSection } from "./user/devices";

export function UserDrawer({ id, onClose }: { id?: number; onClose: () => void }) {
  const user = useUser(id);
  const u = user.data;
  return (
    <Drawer
      open={!!id}
      onOpenChange={(v) => !v && onClose()}
      title={u?.name ?? t("userDrawer.fallbackTitle")}
      lead={u ? <DrawerAvatar key={u.id} id={u.id} name={u.name} /> : undefined}
      meta={
        u ? (
          <>
            <StatePill state={u.state} />
            <span>{t("userDrawer.created", { date: dateShort(u.created_at) })}</span>
            <span className="mono">#{u.id}</span>
            {u.online ? <span className="text-[var(--leaf-700)]">{t("userDrawer.online")}</span> : u.online_at ? <span>{t("userDrawer.seen", { ago: ago(u.online_at) })}</span> : null}
          </>
        ) : undefined
      }
    >
      <QueryBoundary
        query={user}
        pending={
          <div className="space-y-4 pt-5">
            <Skeleton style={{ height: 40 }} />
            <Skeleton style={{ height: 120, borderRadius: 16 }} />
            <Skeleton style={{ height: 160, borderRadius: 16 }} />
          </div>
        }
      >
        {/* A failed poll keeps the card (and the forms in it); the notice sits above it. */}
        {(data) => <UserBody key={data.id} u={data} onDeleted={onClose} />}
      </QueryBoundary>
    </Drawer>
  );
}

function UserBody({ u, onDeleted }: { u: User; onDeleted: () => void }) {  const toast = useToast();
  const extend = useUserMutation(userActions.extend);
  const reset = useUserMutation(userActions.reset);
  const update = useUserMutation(userActions.update);
  const reissue = useUserMutation(userActions.reissue);
  const remove = useUserMutation(userActions.remove);
  const [confirm, setConfirm] = useState<"reissue" | "delete" | null>(null);
  const fail = (e: unknown) => toast.error(errorText(e));
  const disabled = u.state === "disabled";
  const { can } = useGrants();
  const writable = can("users", true);

  return (
    <>
      {writable ? (
      <div className="flex flex-wrap gap-2 py-4">
        <Button variant="primary" loading={extend.isPending} onClick={() => extend.mutate({ id: u.id, ...onePeriod(u) }, { onSuccess: (r) => toast.ok(t("userDrawer.extendedUntil", { date: dateShort(r.expires_at!) })), onError: fail })}>
          <CalendarPlus size={18} aria-hidden /> {u.billing_day != null ? t("userDrawer.extendMonth") : t("userDrawer.extend30")}
        </Button>
        <Button loading={reset.isPending} onClick={() => reset.mutate(u.id, { onSuccess: () => toast.ok(t("userDrawer.trafficReset")), onError: fail })}>
          <RotateCcw size={18} aria-hidden /> {t("users.resetTraffic")}
        </Button>
        <Button
          loading={update.isPending && update.variables?.body.disabled !== undefined}
          onClick={() =>
            update.mutate(
              { id: u.id, body: { disabled: !disabled } },
              { onSuccess: () => toast.ok(disabled ? t("userDrawer.enabledToast", { name: u.name }) : t("userDrawer.disabledToast", { name: u.name })), onError: fail },
            )
          }
        >
          <Power size={18} aria-hidden /> {disabled ? t("common.enable") : t("users.disable")}
        </Button>
        <Menu.Root>
          <Menu.Trigger asChild>
            <button type="button" className="icon-btn h-10 w-10" aria-label={t("userDrawer.more")}>
              <MoreHorizontal size={18} />
            </button>
          </Menu.Trigger>
          <Menu.Portal>
            <Menu.Content className="menu glass-strong" align="end" sideOffset={6}>
              <Menu.Item className="menu-item" onSelect={() => setConfirm("reissue")}>
                <RefreshCw size={16} aria-hidden /> {t("userDrawer.reissue")}
              </Menu.Item>
              <Menu.Separator className="menu-sep" />
              <Menu.Item className="menu-item danger" onSelect={() => setConfirm("delete")}>
                <Trash2 size={16} aria-hidden /> {t("userDrawer.delete")}
              </Menu.Item>
            </Menu.Content>
          </Menu.Portal>
        </Menu.Root>
      </div>
      ) : null}

      <TariffSection u={u} />
      <TrafficSection u={u} />
      <PoolsSection u={u} />
      <GrantsSection u={u} />
      <ExpirySection u={u} />
      <SubscriptionSection u={u} onReissue={() => setConfirm("reissue")} />
      <KeysSection u={u} />
      <TelegramSection u={u} />
      <DevicesSection u={u} />
      <ProtocolsSection u={u} />
      <NoteSection u={u} />

      <Confirm
        open={confirm === "reissue"}
        onOpenChange={(v) => !v && setConfirm(null)}
        title={t("userDrawer.reissueTitle")}
        text={t("userDrawer.reissueText")}
        confirm={t("userDrawer.reissueConfirm")}
        loading={reissue.isPending}
        onConfirm={() =>
          reissue.mutate(u.id, {
            onSuccess: () => {
              setConfirm(null);
              toast.ok(t("userDrawer.reissued"));
            },
            onError: fail,
          })
        }
      />
      <Confirm
        open={confirm === "delete"}
        onOpenChange={(v) => !v && setConfirm(null)}
        title={t("userDrawer.deleteTitle", { name: u.name })}
        text={t("userDrawer.deleteText")}
        confirm={t("common.delete")}
        danger
        loading={remove.isPending}
        onConfirm={() =>
          remove.mutate(u.id, {
            onSuccess: () => {
              setConfirm(null);
              toast.ok(t("userDrawer.deleted", { name: u.name }));
              onDeleted();
            },
              onError: fail,
          })
        }
      />
    </>
  );
}

/**
 * The user's photo in the drawer header: the uploaded file, the initials when there
 * is none (a 404 from the avatar endpoint falls back, no extra API call). The camera
 * button picks a jpeg/png (2 MB max client-side) and POSTs it as base64.
 */
function DrawerAvatar({ id, name }: { id: number; name: string }) {
  const qc = useQueryClient();
  const toast = useToast();
  const { can } = useGrants();
  const writable = can("users", true);
  const [failed, setFailed] = useState(false);
  const [nonce, setNonce] = useState(0);
  const [busy, setBusy] = useState(false);
  const inputRef = useRef<HTMLInputElement>(null);
  const src = basePath + `/api/v1/users/${id}/avatar` + (nonce ? `?v=${nonce}` : "");
  const label = t(failed ? "userDrawer.avatar.upload" : "userDrawer.avatar.replace");

  const onFile = async (f: File | undefined) => {
    if (!f || busy) return;
    if (f.type !== "image/jpeg" && f.type !== "image/png") {
      toast.error(t("userDrawer.avatar.badType"));
      return;
    }
    if (f.size > 2 * 1024 * 1024) {
      toast.error(t("userDrawer.avatar.tooBig"));
      return;
    }
    setBusy(true);
    try {
      const dataUrl = await new Promise<string>((resolve, reject) => {
        const r = new FileReader();
        r.onload = () => resolve(String(r.result));
        r.onerror = () => reject(new Error("read"));
        r.readAsDataURL(f);
      });
      const image = dataUrl.includes(",") ? dataUrl.slice(dataUrl.indexOf(",") + 1) : dataUrl;
      const token = csrf();
      const res = await fetch(basePath + `/api/v1/users/${id}/avatar`, {
        method: "POST",
        credentials: "same-origin",
        headers: { "Content-Type": "application/json", ...(token ? { "X-CSRF-Token": token } : {}) },
        body: JSON.stringify({ image }),
      });
      if (!res.ok) {
        let code = "server";
        try {
          const body = (await res.json()) as { detail?: string };
          if (body.detail) code = body.detail;
        } catch {
          /* keep generic */
        }
        throw new Error(tMaybe(`errors.api.${code}`) ?? code);
      }
      setFailed(false);
      const v = Date.now();
      setNonce(v);
      window.dispatchEvent(new CustomEvent("cozy:avatar", { detail: { id } }));
      await qc.invalidateQueries({ queryKey: qk.user(id) });
      await qc.invalidateQueries({ queryKey: qk.users });
    } catch (e) {
      toast.error(e instanceof Error ? e.message : t("errors.generic"));
    } finally {
      setBusy(false);
      if (inputRef.current) inputRef.current.value = "";
    }
  };

  return (
    <span style={{ position: "relative", display: "inline-block", flex: "none" }}>
      {failed ? (
        <Avatar name={name} seed={id} size="lg" />
      ) : (
        <img src={src} alt="" width={52} height={52} onError={() => setFailed(true)} style={{ width: 52, height: 52, borderRadius: "50%", objectFit: "cover", display: "block" }} />
      )}
      {writable ? (
        <>
          <button
            type="button"
            className="icon-btn"
            onClick={() => inputRef.current?.click()}
            disabled={busy}
            aria-label={label}
            title={label}
            style={{ position: "absolute", right: -4, bottom: -4, width: 24, height: 24, borderRadius: "50%", background: "var(--glass-strong)", border: "1px solid var(--hairline)" }}
          >
            <Camera size={14} aria-hidden />
          </button>
          <input ref={inputRef} type="file" accept="image/jpeg,image/png" hidden aria-label={label} onChange={(e) => void onFile(e.target.files?.[0])} />
        </>
      ) : null}
    </span>
  );
}
