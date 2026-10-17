import { useMutation } from "@tanstack/react-query";
import { ShieldQuestion } from "lucide-react";
import { useState } from "react";
import { api, errorText, unwrap, type Schemas } from "../api/client";
import { useToast } from "./toast";
import { Button, Pill } from "./ui";
import { Drawer } from "./overlay";
import { t, tMaybe } from "../i18n";
import { dateShort, time } from "../lib/format";

const STATUS_TONE = { ok: "ok", info: "off", warn: "warn", fail: "bad" } as const;

/**
 * "Check the certificate": what the panel's and the nodes' ports serve as seen from
 * outside, worst finding first, each with what to do about it.
 */
export function CertCheckDrawer({ onClose }: { onClose: () => void }) {
  const toast = useToast();
  const [report, setReport] = useState<Schemas["CertReport"] | null>(null);
  const run = useMutation({
    mutationFn: () => unwrap(api.POST("/api/v1/settings/certificate/check")),
    onSuccess: (data) => setReport(data),
    onError: (e) => toast.error(errorText(e)),
  });
  const checks = report?.checks ?? [];
  return (
    <Drawer
      open
      onOpenChange={(v) => !v && onClose()}
      title={t("certCheck.title")}
      meta={t("certCheck.lead")}
      footer={
        <Button variant="ghost" onClick={onClose}>
          {t("common.close")}
        </Button>
      }
    >
      <Button variant="primary" className="mb-4" loading={run.isPending} onClick={() => run.mutate()}>
        <ShieldQuestion size={16} aria-hidden /> {t("certCheck.run")}
      </Button>
      {report ? (
        <>
          <p className="mb-3 text-xs text-[var(--ink-500)]">{t("certCheck.at", { date: dateShort(report.checked_at), time: time(report.checked_at) })}</p>
          {checks.length === 0 ? (
            <p className="text-[13px] text-[var(--ink-500)]">{t("certCheck.nothing")}</p>
          ) : (
            <ul className="flex flex-col gap-2">
              {checks.map((c) => (
                <li key={c.id} className="panel-soft flex items-start justify-between gap-3 p-3">
                  <div className="min-w-0">
                    <div className="text-[13px] font-medium">{tMaybe(`certCheck.${c.id}`) ?? c.id}</div>
                    {c.detail ? <div className="mt-0.5 text-xs text-[var(--ink-500)]">{c.detail}</div> : null}
                  </div>
                  <Pill tone={STATUS_TONE[c.status as keyof typeof STATUS_TONE] ?? "off"}>{tMaybe(`certCheck.status.${c.status}`) ?? c.status}</Pill>
                </li>
              ))}
            </ul>
          )}
        </>
      ) : null}
    </Drawer>
  );
}

/** The button that opens the check. */
export function CertCheckButton() {
  const [open, setOpen] = useState(false);
  return (
    <>
      <Button size="sm" onClick={() => setOpen(true)}>
        <ShieldQuestion size={16} aria-hidden /> {t("certCheck.button")}
      </Button>
      {open ? <CertCheckDrawer onClose={() => setOpen(false)} /> : null}
    </>
  );
}