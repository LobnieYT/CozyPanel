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

export type SubPagePart = "look" | "brand" | "blocks" | "apps" | "css";

const PARTS: SubPagePart[] = ["look", "brand", "blocks", "apps", "css"];

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

/** The choices of a Segmented: their own names, no translation: these are values, not words. */
function opt<T extends string>(values: readonly T[]) {
  return values.map((v) => ({ value: v, label: v }));
}

function useSubPage() {
  return useQuery({ queryKey: ["sub-page"], queryFn: ({ signal }) => unwrap(api.GET("/api/v1/sub-page", { signal })) });
}

/** The subscription page: its look, its brand, its blocks, the apps it lists and its CSS. */
export function SubPageSettings({ section, onSection }: { section: SubPagePart; onSection: (s: SubPagePart) => void }) {
  const page = useSubPage();
  return (
    <>
      <nav className="section-parts" aria-label={t("settings.page.title")}>
        {PARTS.map((p) => (
          <button key={p} type="button" aria-current={p === section ? "page" : undefined} onClick={() => onSection(p)}>
            {t(`settings.page.tab.${p}`)}
          </button>
        ))}
      </nav>
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
      {section === "look" ? (
        <>
          <Card title={t("settings.page.lookPalette")}>
            <Field label={t("settings.page.palette")}>
              <Segmented label="" value={config.look.palette} options={opt<Page["look"]["palette"]>(["mikan", "ocean", "sakura", "forest", "graphite", "abyss", "ember", "moss", "nord", "tokyo"])} onChange={(v) => set((c) => ({ ...c, look: { ...c.look, palette: v } }))} />
            </Field>
            <Field label={t("settings.page.mode")}>
              <Segmented label="" value={config.look.mode} options={opt<Page["look"]["mode"]>(["light", "dark", "system"])} onChange={(v) => set((c) => ({ ...c, look: { ...c.look, mode: v } }))} />
            </Field>
            <Field label={t("settings.page.accent")} hint={config.look.accent === "brand" && !accent ? t("settings.page.accentNeeded") : undefined}>
              <Segmented label="" value={config.look.accent} options={opt<Page["look"]["accent"]>(["theme", "brand"])} onChange={(v) => set((c) => ({ ...c, look: { ...c.look, accent: v } }))} />
            </Field>
            <Field label={t("settings.page.background")}>
              <Segmented label="" value={config.look.background.kind} options={opt<Page["look"]["background"]["kind"]>(["theme", "solid", "gradient"])} onChange={(v) => set((c) => ({ ...c, look: { ...c.look, background: { ...c.look.background, kind: v } } }))} />
            </Field>
            {config.look.background.kind !== "theme" ? (
              <div className="grid gap-3 sm:grid-cols-2">
                <Field label={t("settings.page.from")} error={errors["look.background.from"]}>
                  <input className="input mono" value={config.look.background.from} onChange={(e) => set((c) => ({ ...c, look: { ...c.look, background: { ...c.look.background, from: e.target.value } } }))} />
                </Field>
                {config.look.background.kind === "gradient" ? (
                  <Field label={t("settings.page.to")} error={errors["look.background.to"]}>
                    <input className="input mono" value={config.look.background.to} onChange={(e) => set((c) => ({ ...c, look: { ...c.look, background: { ...c.look.background, to: e.target.value } } }))} />
                  </Field>
                ) : null}
              </div>
            ) : null}
            <Field label={t("settings.page.font")}>
              <Segmented label="" value={config.look.font} options={opt<Page["look"]["font"]>(["default", "system", "rounded"])} onChange={(v) => set((c) => ({ ...c, look: { ...c.look, font: v } }))} />
            </Field>
            <Field label={t("settings.page.radius")}>
              <Segmented label="" value={config.look.radius} options={opt<Page["look"]["radius"]>(["small", "medium", "large"])} onChange={(v) => set((c) => ({ ...c, look: { ...c.look, radius: v } }))} />
            </Field>
            <Field label={t("settings.page.cards")}>
              <Segmented label="" value={config.look.cards} options={opt<Page["look"]["cards"]>(["glass", "solid"])} onChange={(v) => set((c) => ({ ...c, look: { ...c.look, cards: v } }))} />
            </Field>
          </Card>
        </>
      ) : section === "brand" ? (
        <Card title={t("settings.page.brandTitle")}>
          <Field label={t("settings.page.brandName")} error={errors.brand}>
            <input className="input" value={brand} maxLength={40} onChange={(e) => setBrand(e.target.value)} />
          </Field>
          <Field label={t("settings.page.brandAccent")} hint={t("settings.page.brandAccentHint")} error={errors.brand_accent}>
            <input className="input mono" value={accent} placeholder="#7C5CFF" onChange={(e) => setAccent(e.target.value)} />
          </Field>
          <Field label={t("settings.page.logo")}>
            <Segmented label="" value={config.brand.logo} options={opt<Page["brand"]["logo"]>(["letter", "emoji", "none"])} onChange={(v) => set((c) => ({ ...c, brand: { ...c.brand, logo: v } }))} />
          </Field>
          {config.brand.logo === "emoji" ? (
            <Field label={t("settings.page.emoji")} error={errors["brand.emoji"]}>
              <input className="input" value={config.brand.emoji} maxLength={32} onChange={(e) => set((c) => ({ ...c, brand: { ...c.brand, emoji: e.target.value } }))} />
            </Field>
          ) : null}
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
