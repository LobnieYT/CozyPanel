import { useMutation } from "@tanstack/react-query";
import { Stethoscope } from "lucide-react";
import { useState } from "react";
import { api, errorText, unwrap, type Schemas } from "../api/client";
import { useToast } from "./toast";
import { Button, Pill } from "./ui";
import { Drawer } from "./overlay";
import { t, tMaybe } from "../i18n";

const TONE = { ok: "ok", warn: "warn", fail: "bad" } as const;

/**
 * "Check the node": what the panel sees when it calls the node, in plain words, with what
 * to do about every finding. The admin reads the broken thing first.
 */
export function NodeCheckDrawer({ node, onClose }: { node: { id: number; name: string }; onClose: () => void }) {
  const toast = useToast();
  const [report, setReport] = useState<Schemas["NodeCheckReport"] | null>(null);
  const run = useMutation({
    mutationFn: () => unwrap(api.POST("/api/v1/nodes/{id}/check", { params: { path: { id: node.id } } })),
    onSuccess: setReport,
    onError: (e) => toast.error(errorText(e)),
  });
  const checks = report?.checks ?? [];
  return (
    <Drawer
      open
      onOpenChange={(v) => !v && onClose()}
      title={t("nodeCheck.title", { name: node.name })}
      meta={t("nodeCheck.lead")}
      footer={
        <>
          <Button variant="ghost" onClick={onClose}>
            {t("common.close")}
          </Button>
          <Button variant="primary" loading={run.isPending} onClick={() => run.mutate()}>
            <Stethoscope size={16} aria-hidden /> {t("nodeCheck.run")}
          </Button>
        </>
      }
    >
      {report ? (
        checks.length === 0 ? (
          <p className="text-[13px] text-[var(--ink-500)]">{t("certCheck.nothing")}</p>
        ) : (
          <ul className="flex flex-col gap-2">
            {checks.map((c) => (
              <li key={c.id} className="panel-soft p-3">
                <div className="flex items-start justify-between gap-3">
                  <span className="text-[13px] font-medium">{tMaybe(`nodeCheck.${c.id}`) ?? c.id}</span>
                  <Pill tone={TONE[c.status as keyof typeof TONE] ?? "off"}>{tMaybe(`certCheck.status.${c.status}`) ?? c.status}</Pill>
                </div>
                {c.detail ? <div className="mt-0.5 text-xs text-[var(--ink-500)]">{c.detail}</div> : null}
                {c.fix ? <div className="mt-1 text-xs text-[var(--berry-600)]">{tMaybe(`nodeCheck.fix.${c.fix}`) ?? c.fix}</div> : null}
              </li>
            ))}
          </ul>
        )
      ) : (
        <p className="text-[13px] text-[var(--ink-500)]">{t("nodeCheck.hint")}</p>
      )}
    </Drawer>
  );
}

/** The button that opens the check on a node card. */
export function NodeCheckButton({ node }: { node: { id: number; name: string } }) {
  const [open, setOpen] = useState(false);
  return (
    <>
      <Button size="sm" onClick={() => setOpen(true)}>
        <Stethoscope size={16} aria-hidden /> {t("nodeCheck.button")}
      </Button>
      {open ? <NodeCheckDrawer node={node} onClose={() => setOpen(false)} /> : null}
    </>
  );
}