import { useState } from "react";
import { useQuery } from "@tanstack/react-query";
import { csrf } from "../../api/client";
import { meQuery } from "../../api/hooks";
import { useToast } from "../../components/toast";
import { Button, Field, PageHeader } from "../../components/ui";
import { t, tMaybe } from "../../i18n";

type Manifest = {
  format: number;
  panel: string;
  created_at: number;
  sections: Record<string, { count: number }>;
};

type Preview = {
  incoming: Record<string, number>;
  overlap: Record<string, number>;
  panel: string;
  created_at: number;
};

type Applied = {
  sections: Record<string, { inserted: number; updated: number; skipped: number }>;
  warnings?: string[];
};

const SECTION_ORDER = ["tariffs", "nodes", "inbounds", "users", "network", "settings"];

function secLabel(s: string): string {
  switch (s) {
    case "tariffs":
      return t("backups.sec_tariffs");
    case "nodes":
      return t("backups.sec_nodes");
    case "inbounds":
      return t("backups.sec_inbounds");
    case "users":
      return t("backups.sec_users");
    case "network":
      return t("backups.sec_network");
    default:
      return t("backups.sec_settings");
  }
}

async function raw(path: string, init?: RequestInit): Promise<Response> {
  return fetch(path, { credentials: "same-origin", ...init });
}

function csrfHeader(): Record<string, string> {
  return csrf() ? { "X-CSRF-Token": csrf() } : {};
}

export function BackupsPage() {
  const toast = useToast();
  const me = useQuery(meQuery);
  const [busy, setBusy] = useState<"export" | "upload" | "apply" | null>(null);
  const [upload, setUpload] = useState<{ id: string; manifest: Manifest } | null>(null);
  const [preview, setPreview] = useState<Preview | null>(null);
  const [picked, setPicked] = useState<Record<string, boolean>>({});
  const [strategy, setStrategy] = useState<"skip" | "replace" | "fresh">("skip");
  const [result, setResult] = useState<Applied | null>(null);

  if (me.data && !me.data.admin.owner) {
    return (
      <>
        <PageHeader title={t("backups.title")} />
        <p className="text-[13px] text-[var(--berry-600)]" role="alert">
          {tMaybe("errors.api.no_grant") ?? "no_grant"}
        </p>
      </>
    );
  }

  const fail = async (res: Response) => {
    let code = "server";
    try {
      const body = (await res.json()) as { detail?: string };
      if (body.detail) code = body.detail;
    } catch {
      /* keep generic */
    }
    toast.error(tMaybe(`errors.api.${code}`) ?? code);
  };

  const doExport = async () => {
    setBusy("export");
    try {
      const res = await raw("/api/v1/backups/export");
      if (!res.ok) {
        await fail(res);
        return;
      }
      const blob = await res.blob();
      const cd = res.headers.get("Content-Disposition") ?? "";
      const m = /filename="([^"]+)"/.exec(cd);
      const a = document.createElement("a");
      a.href = URL.createObjectURL(blob);
      a.download = m?.[1] ?? "cozy-backup.tar.gz";
      a.click();
      setTimeout(() => URL.revokeObjectURL(a.href), 60_000);
    } finally {
      setBusy(null);
    }
  };

  const doUpload = async (file: File) => {
    setBusy("upload");
    setUpload(null);
    setPreview(null);
    setResult(null);
    try {
      const res = await raw("/api/v1/backups/upload", {
        method: "POST",
        headers: { "Content-Type": "application/gzip", ...csrfHeader() },
        body: file,
      });
      if (!res.ok) {
        await fail(res);
        return;
      }
      const up = (await res.json()) as { id: string; manifest: Manifest };
      setUpload(up);
      const all: Record<string, boolean> = {};
      for (const s of Object.keys(up.manifest.sections)) all[s] = true;
      setPicked(all);
      const pv = await raw(`/api/v1/backups/preview?id=${encodeURIComponent(up.id)}`);
      if (!pv.ok) {
        await fail(pv);
        return;
      }
      setPreview((await pv.json()) as Preview);
    } finally {
      setBusy(null);
    }
  };

  const doApply = async () => {
    if (!upload) return;
    setBusy("apply");
    try {
      const res = await raw("/api/v1/backups/apply", {
        method: "POST",
        headers: { "Content-Type": "application/json", ...csrfHeader() },
        body: JSON.stringify({
          id: upload.id,
          sections: SECTION_ORDER.filter((s) => picked[s]),
          strategy,
        }),
      });
      if (!res.ok) {
        await fail(res);
        return;
      }
      setResult((await res.json()) as Applied);
      setUpload(null);
      setPreview(null);
    } finally {
      setBusy(null);
    }
  };

  return (
    <>
      <PageHeader title={t("backups.title")} sub={t("backups.sub")} />
      <section className="card glass max-w-4xl">
        <div className="card-head">
          <div>
            <h2 className="card-title">{t("backups.export")}</h2>
            <div className="card-sub">{t("backups.exportSub")}</div>
          </div>
        </div>
        <Button variant="primary" loading={busy === "export"} onClick={() => void doExport()}>
          {t("backups.download")}
        </Button>
      </section>
      <section className="card glass mt-4 max-w-4xl">
        <div className="card-head">
          <div>
            <h2 className="card-title">{t("backups.import")}</h2>
            <div className="card-sub">{t("backups.importSub")}</div>
          </div>
        </div>
        <Field label={t("backups.file")}>
          <input
            className="input"
            type="file"
            accept=".tar.gz,.tgz"
            disabled={busy !== null}
            onChange={(e) => {
              const f = e.target.files?.[0];
              e.target.value = "";
              if (f) void doUpload(f);
            }}
          />
        </Field>
        {busy === "upload" ? <p className="mb-3 text-[13px] text-[var(--ink-500)]">{t("backups.uploading")}</p> : null}
        {preview && upload ? (
          <>
            <p className="mb-3 text-xs text-[var(--ink-500)]">
              {t("backups.from", { panel: preview.panel, date: new Date(preview.created_at * 1000).toLocaleString() })}
            </p>
            <ul className="mb-4 flex flex-col gap-2">
              {SECTION_ORDER.filter((s) => preview.incoming[s] != null).map((s) => (
                <li key={s} className="flex items-center justify-between gap-3">
                  <label className="flex items-center gap-2 text-[13px]">
                    <input type="checkbox" className="check" checked={!!picked[s]} onChange={(e) => setPicked((p) => ({ ...p, [s]: e.target.checked }))} />
                    {secLabel(s)}
                  </label>
                  <span className="num text-xs text-[var(--ink-500)]">
                    {preview.incoming[s]} · {t("backups.overlap", { n: preview.overlap[s] ?? 0 })}
                  </span>
                </li>
              ))}
            </ul>
            <div className="mb-4 flex flex-wrap gap-2" role="group" aria-label={t("backups.strategy")}>
              {(["skip", "replace", "fresh"] as const).map((v) => (
                <button key={v} type="button" className="chip-btn" aria-pressed={strategy === v} onClick={() => setStrategy(v)} title={t(`backups.strategy_${v}_hint`)}>
                  {t(`backups.strategy_${v}`)}
                </button>
              ))}
            </div>
            <Button variant="primary" loading={busy === "apply"} onClick={() => void doApply()}>
              {t("backups.apply")}
            </Button>
          </>
        ) : null}
        {result ? (
          <div className="mt-4">
            <ul className="flex flex-col gap-1 text-[13px]">
              {Object.entries(result.sections).map(([s, r]) => (
                <li key={s} className="num text-[var(--ink-600)]">
                  {secLabel(s)}: +{r.inserted} ~{r.updated} ={r.skipped}
                </li>
              ))}
            </ul>
            {(result.warnings ?? []).map((w, i) => (
              <p key={i} className="mt-1 text-xs text-[var(--honey-600)]">
                {w}
              </p>
            ))}
          </div>
        ) : null}
      </section>
    </>
  );
}
