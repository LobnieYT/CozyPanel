import { useState } from "react";
import { Button } from "../components/ui";
import { t } from "../i18n";
import { dateShort, time } from "../lib/format";
import { initData, request, subRoot } from "./net";
import type { Info } from "./types";

function period(days: number): string {
  if (days === 1) return t("sub.unbindDay");
  if (days === 7) return t("sub.unbindWeek");
  if (days === 30 || days === 31) return t("sub.unbindMonth");
  return t("sub.unbindDays", { n: days });
}

/** The token of the subscription on screen: the last path segment of its address. */
function tokenOf(subURL: string): string {
  return subURL.split("/").filter(Boolean).pop() ?? "";
}

/**
 * A new link at the subscriber's own request (Mini App only): the old link stops working
 * at once and the apps need the new one, so the button asks first.
 */
export function ReissueSection({ info, subURL, onDone }: { info: Info; subURL: string; onDone: (token: string) => void }) {
  const [open, setOpen] = useState(false);
  const [busy, setBusy] = useState(false);
  const [error, setError] = useState("");
  const [wait, setWait] = useState(info.reissue_after ?? "");
  if (!info.reissue) return null;
  const waiting = !!wait && new Date(wait).getTime() > Date.now();

  const issue = async () => {
    setBusy(true);
    setError("");
    try {
      const r = await request(`${subRoot}/tg/reissue`, {
        method: "POST",
        headers: { "Content-Type": "application/json" },
        body: JSON.stringify({ init_data: initData, token: tokenOf(subURL) }),
      });
      const b = (await r.json().catch(() => ({}))) as { token?: string; code?: string; reissue_after?: string };
      if (r.ok && b.token) {
        setOpen(false);
        onDone(b.token);
        return;
      }
      if (r.status === 429) {
        setWait(b.reissue_after ?? "");
        setError(b.reissue_after ? "" : t("sub.reissueFailed"));
        return;
      }
      setError(b.code === "reissue_off" ? t("sub.reissueOff") : t("sub.reissueFailed"));
    } catch {
      setError(t("sub.reissueFailed"));
    } finally {
      setBusy(false);
    }
  };

  return (
    <section className="glass rounded-3xl p-4">
      <h2 className="text-[15px] font-semibold">{t("sub.reissueTitle")}</h2>
      <p className="mt-1 mb-3 text-[13px] text-[var(--ink-500)]">{t("sub.reissueText")}</p>
      {waiting ? (
        <p className="mb-3 rounded-2xl bg-[var(--hover)] p-3 text-[13px] text-[var(--ink-700)]" role="status">
          {t("sub.reissueAfter", { date: dateShort(wait), time: time(wait) })}
        </p>
      ) : null}
      {!open ? (
        <Button id="reissue-open" variant="primary" disabled={busy || waiting} onClick={() => setOpen(true)}>
          {t("sub.reissueButton")}
        </Button>
      ) : (
        <div>
          <p className="mb-3 rounded-2xl bg-[var(--hover)] p-3 text-[13px] text-[var(--ink-700)]">{t("sub.reissueWarn")}</p>
          <div className="flex justify-end gap-2">
            <Button variant="ghost" disabled={busy} onClick={() => setOpen(false)}>
              {t("common.cancel")}
            </Button>
            <Button variant="danger-solid" loading={busy} onClick={() => void issue()}>
              {t("sub.reissueYes")}
            </Button>
          </div>
        </div>
      )}
      {error ? (
        <p className="mt-2 text-[13px] text-[var(--berry-600)]" role="alert">
          {error}
        </p>
      ) : null}
      {!info.reissue_off && (info.reissue_limit ?? 0) > 0 ? (
        <p className="mt-2 text-xs text-[var(--ink-500)]">{t("sub.reissueLimit", { n: info.reissue_limit ?? 0, period: period(info.reissue_days ?? 1) })}</p>
      ) : null}
    </section>
  );
}