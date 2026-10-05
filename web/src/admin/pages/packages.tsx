// Traffic packages (GitHub issue #12): extra traffic for the main quota or one pool,
// given by the admin. The base quota of the period is spent first, then the packages,
// the soonest to expire first.
import { useMutation, useQueryClient } from "@tanstack/react-query";
import { Archive, Pencil, Plus } from "lucide-react";
import { useEffect, useState, type FormEvent } from "react";
import { api, ApiError, errorText, unwrap, type Schemas } from "../../api/client";
import { qk, useGrants, usePackages, usePools } from "../../api/hooks";
import { Confirm, Drawer } from "../../components/overlay";
import { QueryBoundary } from "../../components/query";
import { useToast } from "../../components/toast";
import { Button, Field, Segmented, Skeleton } from "../../components/ui";
import { t } from "../../i18n";
import { bytes, days as daysText, GiB } from "../../lib/format";

type Package = Schemas["PackageView"];
type Pool = Schemas["PoolView"];
export type Lifetime = Package["lifetime"];

/** Where a package or a grant adds traffic: the main quota or a pool by name. */
export function targetName(poolId: number | null | undefined, pools: Pool[] | undefined): string {
  if (poolId == null) return t("pools.mainTraffic");
  return pools?.find((p) => p.id === poolId)?.name ?? t("packages.poolGone");
}

/** How long a package lasts: until used up, until the period ends, or N days. */
function lifetimeText(lifetime: Lifetime, days: number): string {
  if (lifetime === "days") return t("packages.lifetimeDaysN", { days: daysText(days) });
  return t(`packages.lifetime.${lifetime}`);
}

export function PackagesCard() {
  const packages = usePackages();
  const pools = usePools();
  const { can } = useGrants();
  const writable = can("tariffs", true);
  const qc = useQueryClient();
  const toast = useToast();
  const [edit, setEdit] = useState<Package | "new" | null>(null);
  const [archive, setArchive] = useState<Package | null>(null);
  const remove = useMutation({
    mutationFn: (id: number) => unwrap(api.DELETE("/api/v1/packages/{id}", { params: { path: { id } } })),
    onSuccess: () => {
      void qc.invalidateQueries({ queryKey: qk.packages });
      setArchive(null);
      toast.ok(t("packages.archived"));
    },
    onError: (e) => toast.error(errorText(e)),
  });

  return (
    <section className="card glass reveal">
      <div className="card-head">
        <div>
          <h2 className="card-title">{t("packages.title")}</h2>
          <div className="card-sub">{t("packages.sub")}</div>
        </div>
        {writable ? (
          <Button size="sm" onClick={() => setEdit("new")}>
            <Plus size={16} aria-hidden /> {t("packages.add")}
          </Button>
        ) : null}
      </div>
      <QueryBoundary query={packages} pending={<Skeleton style={{ height: 64 }} />}>
        {(list) =>
          list.length === 0 ? (
            <p className="text-xs text-[var(--ink-500)]">{t("packages.none")}</p>
          ) : (
            <ul className="row-list">
              {list.map((p) => (
                <li key={p.id} className="flex flex-wrap items-center justify-between gap-3 py-3">
                  <div className="min-w-0">
                    <div className="text-[13px] font-semibold">{p.name}</div>
                    <div className="text-xs text-[var(--ink-500)]">
                      {[bytes(p.bytes), targetName(p.pool_id, pools.data), lifetimeText(p.lifetime, p.days)].join(" · ")}
                    </div>
                  </div>
                  <div className="flex gap-1">
                    {writable ? (
                      <>
                        <button type="button" className="icon-btn" aria-label={t("packages.editLabel", { name: p.name })} onClick={() => setEdit(p)}>
                          <Pencil size={16} />
                        </button>
                        <button type="button" className="icon-btn" aria-label={t("packages.archiveLabel", { name: p.name })} onClick={() => setArchive(p)}>
                          <Archive size={16} />
                        </button>
                      </>
                    ) : null}
                  </div>
                </li>
              ))}
            </ul>
          )
        }
      </QueryBoundary>
      <PackageDrawer pkg={edit} pools={pools.data ?? []} onClose={() => setEdit(null)} />
      <Confirm
        open={!!archive}
        onOpenChange={(v) => !v && setArchive(null)}
        title={t("packages.archiveTitle", { name: archive?.name ?? "" })}
        text={t("packages.archiveText")}
        confirm={t("tariffs.archiveConfirm")}
        loading={remove.isPending}
        onConfirm={() => archive && remove.mutate(archive.id)}
      />
    </section>
  );
}

function PackageDrawer({ pkg, pools, onClose }: { pkg: Package | "new" | null; pools: Pool[]; onClose: () => void }) {
  const qc = useQueryClient();
  const toast = useToast();
  const [name, setName] = useState("");
  const [gb, setGb] = useState("50");
  const [pool, setPool] = useState(0);
  const [lifetime, setLifetime] = useState<Lifetime>("used");
  const [days, setDays] = useState("30");
  const [errors, setErrors] = useState<Record<string, string>>({});

  useEffect(() => {
    if (!pkg) return;
    const p = pkg === "new" ? null : pkg;
    setName(p?.name ?? "");
    setGb(p ? String(+(p.bytes / GiB).toFixed(2)) : "50");
    setPool(p?.pool_id ?? 0);
    setLifetime(p?.lifetime ?? "used");
    setDays(String(p?.days || 30));
    setErrors({});
  }, [pkg]);

  const save = useMutation({
    mutationFn: (body: Schemas["PackageBody"]) =>
      pkg === "new" || !pkg ? unwrap(api.POST("/api/v1/packages", { body })) : unwrap(api.PUT("/api/v1/packages/{id}", { params: { path: { id: pkg.id } }, body })),
    onSuccess: () => {
      void qc.invalidateQueries({ queryKey: qk.packages });
      toast.ok(pkg === "new" ? t("packages.created") : t("packages.saved"));
      onClose();
    },
    onError: (e) => {
      if (e instanceof ApiError && Object.keys(e.fields).length) setErrors(e.fields);
      else toast.error(errorText(e));
    },
  });

  const submit = (e: FormEvent) => {
    e.preventDefault();
    const errs: Record<string, string> = {};
    const gbN = Number(gb.replace(",", "."));
    const daysN = Number(days);
    if (!name.trim()) errs.name = t("packages.errName");
    if (!Number.isFinite(gbN) || gbN < 1 || gbN > 102400) errs.bytes = t("errors.api.bad_bytes");
    if (lifetime === "days" && (!Number.isInteger(daysN) || daysN < 1 || daysN > 3650)) errs.days = t("errors.api.bad_days");
    setErrors(errs);
    if (Object.keys(errs).length) return;
    save.mutate({
      name: name.trim(),
      bytes: Math.round(gbN * GiB),
      pool_id: pool || undefined,
      lifetime,
      days: lifetime === "days" ? daysN : undefined,
      // PUT replaces the package: keep its place in the list.
      sort: pkg && pkg !== "new" ? pkg.sort : undefined,
    });
  };

  return (
    <Drawer
      open={!!pkg}
      onOpenChange={(v) => !v && onClose()}
      title={pkg === "new" ? t("packages.new") : t("packages.edit")}
      meta={t("packages.drawerMeta")}
      footer={
        <>
          <Button variant="ghost" onClick={onClose}>
            {t("common.cancel")}
          </Button>
          <Button variant="primary" type="submit" form="package-form" loading={save.isPending}>
            {t("common.save")}
          </Button>
        </>
      }
    >
      <form id="package-form" onSubmit={submit} className="pt-5" noValidate>
        <Field label={t("packages.name")} htmlFor="pk-name" error={errors.name}>
          <input id="pk-name" className="input" value={name} onChange={(e) => setName(e.target.value)} maxLength={60} placeholder={t("packages.namePlaceholder")} aria-invalid={!!errors.name} autoFocus />
        </Field>
        <Field label={t("packages.size")} htmlFor="pk-gb" error={errors.bytes}>
          <div className="flex items-center gap-2">
            <input id="pk-gb" className="input max-w-[140px]" inputMode="decimal" value={gb} onChange={(e) => setGb(e.target.value)} aria-invalid={!!errors.bytes} />
            <span className="text-[var(--ink-500)]">{t("units.gb")}</span>
          </div>
        </Field>
        <Field label={t("packages.target")} htmlFor="pk-pool" hint={t("packages.targetHint")} error={errors.pool_id}>
          <select id="pk-pool" className="input max-w-[320px]" value={pool} onChange={(e) => setPool(Number(e.target.value))}>
            <option value={0}>{t("pools.mainTraffic")}</option>
            {pools.map((p) => (
              <option key={p.id} value={p.id}>
                {p.name}
              </option>
            ))}
          </select>
        </Field>
        <Field label={t("packages.lifetimeLabel")} htmlFor={lifetime === "days" ? "pk-days" : undefined} hint={t(`packages.lifetimeHint.${lifetime}`)} error={errors.days ?? errors.lifetime}>
          <Segmented
            label={t("packages.lifetimeLabel")}
            value={lifetime}
            onChange={setLifetime}
            options={[
              { value: "used", label: t("packages.lifetime.used") },
              { value: "period", label: t("packages.lifetime.period") },
              { value: "days", label: t("packages.lifetimeDays") },
            ]}
          />
          {lifetime === "days" ? (
            <div className="mt-2 flex flex-wrap items-center gap-2">
              <input id="pk-days" className="input max-w-[100px]" inputMode="numeric" value={days} onChange={(e) => setDays(e.target.value)} aria-invalid={!!errors.days} />
              {[7, 30, 90].map((n) => (
                <button key={n} type="button" className="chip-btn" onClick={() => setDays(String(n))}>
                  {daysText(n)}
                </button>
              ))}
            </div>
          ) : null}
        </Field>
      </form>
    </Drawer>
  );
}
