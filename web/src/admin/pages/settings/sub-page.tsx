import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { useState, type FormEvent } from "react";
import { api, errorText, unwrap, type Schemas } from "../../../api/client";
import { QueryBoundary } from "../../../components/query";
import { Switch } from "../../../components/switch";
import { Button, Field, Segmented, Skeleton } from "../../../components/ui";
import { useToast } from "../../../components/toast";
import { t, tMaybe, type Key } from "../../../i18n";
import { fieldErrors } from "../../../lib/fields";

type Page = Schemas["Page"];

export type SubPagePart = "brand" | "blocks" | "apps" | "css";

const BLOCK_TYPES: Record<string, Key> = {
  announce: "settings.page.block.announce",
  status: "settings.page.block.status",
  promo: "settings.page.block.promo",
  traffic: "settings.page.block.traffic",
  devices: "settings.page.block.devices",
  apps: "settings.page.block.apps",
  guide: "settings.page.block.guide",
  instructions: "settings.page.block.instructions",
  link: "settings.page.block.link",
  reissue: "settings.page.block.reissue",
  locations: "settings.page.block.locations",
  telegram: "settings.page.block.telegram",
  support: "settings.page.block.support",
  text: "settings.page.block.text",
  links: "settings.page.block.links",
};

const PLATFORMS = ["ios", "android", "windows", "macos", "linux"] as const;

function useSubPage() {
  return useQuery({ queryKey: ["sub-page"], queryFn: ({ signal }) => unwrap(api.GET("/api/v1/sub-page", { signal })) });
}

/** The subscription page: its look, its brand, its blocks, the apps it lists and its CSS. */
export function SubPageSettings({ section }: { section: SubPagePart }) {
  const page = useSubPage();
  return (
    <>
      <QueryBoundary query={page} pending={<Skeleton style={{ height: 420, borderRadius: 20 }} />}>
        {(v) => <Editor v={v} section={section} />}
      </QueryBoundary>
    </>
  );
}

function Editor({ v, section }: { v: Schemas["SubPageView"]; section: SubPagePart }) {
  const qc = useQueryClient();
  const toast = useToast();
  const [config, setConfig] = useState<Page>(v.config);
  const [brand, setBrand] = useState(v.brand);
  const [accent, setAccent] = useState(v.brand_accent);
  const save = useMutation({
    mutationFn: () => unwrap(api.PUT("/api/v1/sub-page", { body: { config, brand, brand_accent: accent } })),
    onSuccess: (data) => {
      qc.setQueryData(["sub-page"], data);
      void qc.invalidateQueries({ queryKey: ["settings"] });
      toast.ok(t("settings.page.saved"));
    },
    onError: (e) => toast.error(errorText(e)),
  });
  const errors = fieldErrors(save.error);
  const set = (f: (c: Page) => Page) => setConfig((c) => f({ ...c }));
  const reset = () => {
    setConfig(v.defaults);
    setBrand(v.brand);
    setAccent(v.brand_accent);
  };
  const submit = (e: FormEvent) => {
    e.preventDefault();
    save.mutate();
  };
  return (
    <form onSubmit={submit} noValidate className="flex flex-col gap-4">
      {section === "brand" ? (
        <Card title={t("settings.page.brandTitle")}>
          <Field label={t("settings.page.brandName")} error={errors.brand}>
            <input className="input" value={brand} maxLength={40} onChange={(e) => setBrand(e.target.value)} />
          </Field>
          <Field label={t("settings.page.brandAccent")} hint={t("settings.page.brandAccentHint")} error={errors.brand_accent}>
            <input className="input mono" value={accent} placeholder="#7C5CFF" onChange={(e) => setAccent(e.target.value)} />
          </Field>
          <Field label={t("settings.page.logo")}>
            <Segmented
              label=""
              value={config.brand.logo}
              options={[
                { value: "letter", label: t("settings.page.signLetter") },
                { value: "emoji", label: t("settings.page.signEmoji") },
                { value: "image", label: t("settings.page.signImage") },
                { value: "none", label: t("settings.page.signNone") },
              ] satisfies { value: Page["brand"]["logo"]; label: string }[]}
              onChange={(v) => set((c) => ({ ...c, brand: { ...c.brand, logo: v } }))}
            />
          </Field>
          {config.brand.logo === "emoji" ? (
            <Field label={t("settings.page.emoji")} error={errors["brand.emoji"]}>
              <input className="input" value={config.brand.emoji} maxLength={32} onChange={(e) => set((c) => ({ ...c, brand: { ...c.brand, emoji: e.target.value } }))} />
            </Field>
          ) : null}
          {config.brand.logo === "image" ? <LogoUpload logo={v.logo} /> : null}
          <Field label={t("settings.page.subtitle")} error={errors["brand.subtitle"]}>
            <input className="input" value={config.brand.subtitle} maxLength={80} onChange={(e) => set((c) => ({ ...c, brand: { ...c.brand, subtitle: e.target.value } }))} />
          </Field>
        </Card>
      ) : section === "blocks" ? (
        <Card title={t("settings.page.blocksTitle")} hint={t("settings.page.blocksHint")}>
          <ul className="flex flex-col gap-2">
            {config.blocks.map((b, i) => (
              <li key={b.id} className="panel-soft flex items-center gap-3 p-3">
                <span className="min-w-0 flex-1 truncate text-[13px]">{t(BLOCK_TYPES[b.type] ?? "settings.page.block.text")}</span>
                <Switch checked={b.on} label="" onChange={(on) => set((c) => ({ ...c, blocks: c.blocks.map((x, j) => (j === i ? { ...x, on } : x)) }))} />
                <Button size="sm" disabled={i === 0} onClick={() => set((c) => ({ ...c, blocks: swap(c.blocks, i, i - 1) }))} aria-label={t("settings.page.up")}>
                  ↑
                </Button>
                <Button size="sm" disabled={i === config.blocks.length - 1} onClick={() => set((c) => ({ ...c, blocks: swap(c.blocks, i, i + 1) }))} aria-label={t("settings.page.down")}>
                  ↓
                </Button>
              </li>
            ))}
          </ul>
          <div className="flex flex-wrap gap-2">
            <Button size="sm" onClick={() => set((c) => ({ ...c, blocks: [...c.blocks, { id: `text-${Date.now().toString(36)}`, type: "text", on: true, title: "", text: "" }] }))}>
              {t("settings.page.addText")}
            </Button>
            <Button size="sm" onClick={() => set((c) => ({ ...c, blocks: [...c.blocks, { id: `links-${Date.now().toString(36)}`, type: "links", on: true, title: "", links: [] }] }))}>
              {t("settings.page.addLinks")}
            </Button>
          </div>
          {config.blocks.filter((b) => b.type === "text" && b.on).map((b) => (
            <Field key={b.id} label={t("settings.page.textOf", { name: b.id })} error={errors[`blocks.${b.id}.text`]}>
              <textarea className="input min-h-[120px]" value={b.text ?? ""} onChange={(e) => set((c) => ({ ...c, blocks: c.blocks.map((x) => (x.id === b.id ? { ...x, text: e.target.value } : x)) }))} />
            </Field>
          ))}
        </Card>
      ) : section === "apps" ? (
        <Card title={t("settings.page.appsTitle")} hint={t("settings.page.appsHint")}>
          <Field label={t("settings.page.platform")}>
            <Segmented label="" value={config.apps.platform} options={opt<Page["apps"]["platform"]>(["auto", ...PLATFORMS])} onChange={(v) => set((c) => ({ ...c, apps: { ...c.apps, platform: v } }))} />
          </Field>
          <Switch checked={config.apps.qr} label={t("settings.page.qr")} onChange={(qr) => set((c) => ({ ...c, apps: { ...c.apps, qr } }))} />
          <ul className="flex flex-col gap-2">
            {config.blocks
              .filter((b) => b.type === "text" || b.type === "links")
              .map((b) => (
                <li key={b.id} className="flex items-center justify-between gap-2">
                  <span className="mono text-xs">{b.id}</span>
                  <Switch
                    checked={b.on}
                    label={t("settings.page.show")}
                    onChange={(on) => set((c) => ({ ...c, blocks: c.blocks.map((x) => (x.id === b.id ? { ...x, on } : x)) }))}
                  />
                </li>
              ))}
          </ul>
        </Card>
      ) : (
        <Card title={t("settings.page.cssTitle")} hint={t("settings.page.cssHint")}>
          <textarea className="input mono min-h-[260px]" value={config.css} onChange={(e) => set((c) => ({ ...c, css: e.target.value }))} spellCheck={false} />
          {errors.css ? <p className="text-xs text-[var(--berry-600)]">{tMaybe(`errors.api.${errors.css}`) ?? errors.css}</p> : null}
        </Card>
      )}
      {Object.keys(errors).length ? (
        <div className="state-box" role="alert">
          {Object.entries(errors)
            .slice(0, 4)
            .map(([f, code]) => (
              <p key={f} className="text-xs text-[var(--berry-600)]">
                {f}: {tMaybe(`errors.api.${code}`) ?? code}
              </p>
            ))}
        </div>
      ) : null}
      <div className="savebar">
        <Button type="button" variant="ghost" onClick={reset}>
          {t("settings.page.reset")}
        </Button>
        <Button type="submit" variant="primary" loading={save.isPending}>
          {t("common.save")}
        </Button>
      </div>
    </form>
  );
}

// The logo as a picture of its own: the file goes to the panel, which keeps it under
// data/subpage and shows it on the subscription page instead of the brand's first letter.
const LOGO_MAX_BYTES = 512 * 1024;
const LOGO_TYPES = ["image/png", "image/jpeg", "image/webp"];

function LogoUpload({ logo }: { logo?: Schemas["PageAsset"] }) {
  const qc = useQueryClient();
  const toast = useToast();
  const [busy, setBusy] = useState(false);
  const [preview, setPreview] = useState<string | null>(null);
  const upload = useMutation({
    mutationFn: (image: string) => unwrap(api.PUT("/api/v1/sub-page/logo", { body: { image } })),
    onSuccess: () => {
      void qc.invalidateQueries({ queryKey: ["sub-page"] });
      void qc.invalidateQueries({ queryKey: ["sub-page-logo"] });
      toast.ok(t("settings.page.logoSaved"));
    },
    onError: (e) => toast.error(errorText(e)),
  });
  const remove = useMutation({
    mutationFn: () => unwrap(api.DELETE("/api/v1/sub-page/logo")),
    onSuccess: () => {
      void qc.invalidateQueries({ queryKey: ["sub-page"] });
      void qc.invalidateQueries({ queryKey: ["sub-page-logo"] });
      setPreview(null);
      toast.ok(t("settings.page.logoRemoved"));
    },
    onError: (e) => toast.error(errorText(e)),
  });
  const pick = async (file: File | undefined) => {
    if (!file) return;
    if (!LOGO_TYPES.includes(file.type)) {
      toast.error(t("settings.page.logoBadType"));
      return;
    }
    if (file.size > LOGO_MAX_BYTES) {
      toast.error(t("settings.page.logoTooBig"));
      return;
    }
    setBusy(true);
    setPreview(URL.createObjectURL(file));
    try {
      const buf = new Uint8Array(await file.arrayBuffer());
      // Кусочками: файл до 512 КБ, но в один длинный base64-аргумент не влезть не должно.
      let bin = "";
      for (let i = 0; i < buf.length; i += 0x8000) {
        bin += String.fromCharCode(...buf.subarray(i, i + 0x8000));
      }
      upload.mutate(btoa(bin));
    } catch {
      setPreview(null);
      toast.error(t("settings.page.logoReadFailed"));
    } finally {
      setBusy(false);
    }
  };
  const shown = useQuery({
    queryKey: ["sub-page-logo", logo?.hash],
    queryFn: () => unwrap(api.GET("/api/v1/sub-page/logo")),
    enabled: !!logo && !preview,
    retry: false,
  });
  const remote = shown.data ? `data:${shown.data.content_type};base64,${shown.data.image}` : null;
  const src = preview ?? remote;
  return (
    <Field label={t("settings.page.logoUpload")} hint={t("settings.page.logoHint")}>
      <div className="flex flex-wrap items-center gap-3">
        <div className="grid h-16 w-16 flex-none place-items-center overflow-hidden rounded-2xl border border-[var(--hairline)] bg-[var(--surface-soft)]">
          {src ? <img src={src} alt={t("settings.page.logoAlt")} className="h-full w-full object-contain" /> : <span className="text-xs text-[var(--ink-500)]">{t("settings.page.logoNone")}</span>}
        </div>
        <div className="flex flex-wrap gap-2">
          <label className="btn cursor-pointer">
            {t("settings.page.logoChoose")}
            <input
              type="file"
              accept={LOGO_TYPES.join(",")}
              className="sr-only"
              disabled={busy || upload.isPending}
              onChange={(e) => void pick(e.target.files?.[0])}
            />
          </label>
          {logo ? (
            <Button type="button" variant="danger" loading={remove.isPending} onClick={() => remove.mutate()}>
              {t("settings.page.logoRemove")}
            </Button>
          ) : null}
        </div>
      </div>
    </Field>
  );
}

/** The choices of a Segmented: their own names, no translation — these are values, not words. */
function opt<T extends string>(values: readonly T[]) {
  return values.map((v) => ({ value: v, label: v }));
}

function swap<T>(list: T[], a: number, b: number): T[] {
  const out = [...list];
  [out[a], out[b]] = [out[b]!, out[a]!];
  return out;
}

function Card({ title, hint, children }: { title: string; hint?: string; children: React.ReactNode }) {
  return (
    <section className="card glass">
      <div className="card-head">
        <div>
          <h2 className="card-title">{title}</h2>
          {hint ? <div className="card-sub">{hint}</div> : null}
        </div>
      </div>
      <div className="grid gap-3">{children}</div>
    </section>
  );
}
