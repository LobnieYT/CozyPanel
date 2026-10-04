// Node network: per-node DNS/routes/outbounds overrides (empty inherits the shared
// ones), the effective setup, a route tester and outbound probes.
import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { useState, type FormEvent } from "react";
import { api, ApiError, errorText, unwrap, type Schemas } from "../../api/client";
import { Drawer } from "../../components/overlay";
import { QueryBoundary } from "../../components/query";
import { Switch } from "../../components/switch";
import { Tabs } from "../../components/tabs";
import { useToast } from "../../components/toast";
import { Button, Field, Pill, Skeleton } from "../../components/ui";
import { t } from "../../i18n";
import { fieldErrors } from "../../lib/fields";
import {
  DeleteConfirm,
  DNSEditor,
  OutboundAddModal,
  OutboundImportModal,
  ROUTE_TARGETS,
  RoutesEditor,
  dnsToJson,
  joinDefault,
  parseDnsDoc,
  parseOutbounds,
  splitDefault,
  type DnsDoc,
  type KVRow,
  type OutboundDoc,
} from "./network";

type NodeNet = Schemas["NodeNetworkView"];
type Probe = Schemas["ProbeView"];

const TABS = ["dns", "routes", "outbounds", "tester", "geo"] as const;
type Tab = (typeof TABS)[number];

function useNodeNet(id: number | null) {
  return useQuery({
    queryKey: ["node-network", id],
    queryFn: ({ signal }) => unwrap(api.GET("/api/v1/nodes/{id}/network", { params: { path: { id: id! } }, signal })),
    enabled: id != null,
  });
}

export function NodeNetworkDrawer({ node, onClose }: { node: { id: number; name: string } | null; onClose: () => void }) {
  const [tab, setTab] = useState<Tab>("dns");
  const net = useNodeNet(node?.id ?? null);
  return (
    <Drawer open={!!node} onOpenChange={(v) => !v && onClose()} title={t("network.title")} meta={node?.name}>
      <div className="pt-5">
        <QueryBoundary query={net} pending={<Skeleton style={{ height: 240, borderRadius: 16 }} />}>
          {(n) => (
            <>
              <Tabs
                id="node-network"
                label={t("network.title")}
                tabs={TABS.map((id) => ({ id, label: t(`network.${id}`) }))}
                value={tab}
                onChange={setTab}
              >
                {tab === "dns" ? (
                  <NodeDNS key="dns" nodeId={node!.id} n={n} />
                ) : tab === "routes" ? (
                  <NodeRoutes key="routes" nodeId={node!.id} n={n} />
                ) : tab === "outbounds" ? (
                  <NodeOutbounds key="outbounds" nodeId={node!.id} n={n} />
                ) : tab === "tester" ? (
                  <NodeTester key="tester" nodeId={node!.id} />
                ) : (
                  <NodeGeo key="geo" nodeId={node!.id} />
                )}
              </Tabs>
            </>
          )}
        </QueryBoundary>
      </div>
    </Drawer>
  );
}

function useSaveNodeNet(nodeId: number) {
  const qc = useQueryClient();
  const toast = useToast();
  return useMutation({
    mutationFn: (body: Schemas["PatchNodeNetworkInputBody"]) =>
      unwrap(api.PATCH("/api/v1/nodes/{id}/network", { params: { path: { id: nodeId } }, body })),
    onSuccess: (data) => {
      qc.setQueryData(["node-network", nodeId], data);
      toast.ok(t("settings.saved"));
    },
    onError: (e) => {
      if (!(e instanceof ApiError && Object.keys(e.fields).length)) toast.error(errorText(e));
    },
  });
}

function InheritRow({ on, onChange }: { on: boolean; onChange: (v: boolean) => void }) {
  return (
    <div className="mb-4 flex items-center justify-between gap-3">
      <div>
        <div className="text-[13px] font-semibold">{t("network.inherit")}</div>
        <div className="text-xs text-[var(--ink-500)]">{t("network.inheritHint")}</div>
      </div>
      <Switch checked={on} onChange={onChange} label={t("network.inherit")} />
    </div>
  );
}

function NodeDNS({ nodeId, n }: { nodeId: number; n: NodeNet }) {
  const save = useSaveNodeNet(nodeId);
  const [inherit, setInherit] = useState(!n.dns_override.trim());
  const initial = parseDnsDoc(n.dns_override);
  const [form, setForm] = useState<DnsDoc>(initial.form);
  const [hostRows, setHostRows] = useState<KVRow[]>(initial.hostRows);
  const errors = fieldErrors(save.error);
  const submit = (e: FormEvent) => {
    e.preventDefault();
    save.mutate({ dns_override: inherit ? "" : dnsToJson(form, hostRows) });
  };
  const set = (k: keyof DnsDoc) => (v: DnsDoc[keyof DnsDoc]) => setForm((f) => ({ ...f, [k]: v }));
  return (
    <form onSubmit={submit} noValidate>
      <InheritRow on={inherit} onChange={setInherit} />
      {!inherit ? (
        <DNSEditor form={form} onForm={set} hostRows={hostRows} onHostRows={setHostRows} />
      ) : (
        <EffectiveDNS n={n} />
      )}
      {errors.dns_override ? (
        <p className="mb-3 text-xs text-[var(--berry-600)]" role="alert">
          {errors.dns_override}
        </p>
      ) : null}
      <Button type="submit" variant="primary" loading={save.isPending}>
        {t("network.save")}
      </Button>
    </form>
  );
}

function EffectiveDNS({ n }: { n: NodeNet }) {
  const d = n.effective_dns;
  if (!d?.enable) return <p className="mb-3 text-[13px] text-[var(--ink-500)]">{t("network.empty")}</p>;
  return (
    <div className="mb-3 text-[13px]">
      <div className="mb-1 flex flex-wrap gap-2">
        <Pill tone="ok">{t("network.enable")}</Pill>
        {d.ipv6 ? <Pill tone="off">IPv6</Pill> : null}
      </div>
      {(d.servers ?? []).map((s, i) => (
        <div key={i} className="mono text-xs text-[var(--ink-600)]">
          {s.address}
          {s.tag ? ` (${s.tag})` : ""}
          {(s.domains ?? []).length ? ` → ${(s.domains ?? []).join(", ")}` : ""}
        </div>
      ))}
    </div>
  );
}

function NodeRoutes({ nodeId, n }: { nodeId: number; n: NodeNet }) {
  const save = useSaveNodeNet(nodeId);
  const [inherit, setInherit] = useState(!n.routes_override.trim());
  const parsed = splitDefault(n.routes_override);
  const [def, setDef] = useState(parsed.def || "DIRECT");
  const [text, setText] = useState(parsed.rules);
  const errors = fieldErrors(save.error);
  const submit = (e: FormEvent) => {
    e.preventDefault();
    save.mutate({ routes_override: inherit ? "" : joinDefault(def, text) });
  };
  return (
    <form onSubmit={submit} noValidate>
      <InheritRow on={inherit} onChange={setInherit} />
      {!inherit ? (
        <RoutesEditor def={def} onDef={setDef} text={text} onText={setText} targets={ROUTE_TARGETS} error={errors.routes_override} />
      ) : (
        <div className="mb-3 text-[13px] text-[var(--ink-600)]">
          {n.effective_default ? `# default: ${n.effective_default}` : null}
          <pre className="mono mt-1 text-xs whitespace-pre-wrap">{n.effective_routes.map((r) => r.rule).join("\n") || t("network.empty")}</pre>
        </div>
      )}
      <Button type="submit" variant="primary" loading={save.isPending}>
        {t("network.save")}
      </Button>
    </form>
  );
}

function NodeOutbounds({ nodeId, n }: { nodeId: number; n: NodeNet }) {
  const save = useSaveNodeNet(nodeId);
  const qc = useQueryClient();
  const toast = useToast();
  const [inherit, setInherit] = useState(!n.outbounds_override.trim());
  const [text, setText] = useState(n.outbounds_override);
  const [showAdd, setShowAdd] = useState(false);
  const [showImport, setShowImport] = useState(false);
  const [confirm, setConfirm] = useState<string | null>(null);
  const [probes, setProbes] = useState<Record<string, { busy: boolean; res?: Probe; err?: string }>>({});
  const list = inherit ? effectiveOutbounds(n) : parseOutbounds(text);
  const errors = fieldErrors(save.error);
  const submit = (e: FormEvent) => {
    e.preventDefault();
    save.mutate({ outbounds_override: inherit ? "" : text });
  };
  const remove = (name: string) => {
    setText(JSON.stringify(parseOutbounds(text).filter((o) => o.name !== name), null, 2));
    setConfirm(null);
  };
  const probe = (proxy: string) => {
    setProbes((p) => ({ ...p, [proxy]: { busy: true } }));
    void unwrap(api.POST("/api/v1/nodes/{id}/probe", { params: { path: { id: nodeId } }, body: { proxy } })).then(
      (res) => {
        setProbes((p) => ({ ...p, [proxy]: { busy: false, res } }));
        void qc.invalidateQueries({ queryKey: ["node-network", nodeId] });
      },
      (e: unknown) => setProbes((p) => ({ ...p, [proxy]: { busy: false, err: errorText(e) } })),
    );
  };
  return (
    <form onSubmit={submit} noValidate>
      <InheritRow
        on={inherit}
        onChange={(v) => {
          setInherit(v);
          if (!v && !text.trim()) setText(JSON.stringify(n.effective_outbounds, null, 2));
        }}
      />
      {!inherit ? (
        <>
          {list.length === 0 ? (
            <p className="mb-3 text-[13px] text-[var(--ink-500)]">{t("network.empty")}</p>
          ) : (
            <ul className="row-list mb-3">
              {list.map((o) => (
                <OutboundRow key={o.name} o={o} probe={probes[o.name]} onProbe={() => probe(o.name)} onDelete={() => setConfirm(o.name)} />
              ))}
            </ul>
          )}
          <div className="mb-3 flex flex-wrap gap-2">
            <Button
              variant="glass"
              onClick={() => {
                setText(JSON.stringify([...parseOutbounds(text), { name: "", type: "socks5", server: "127.0.0.1", port: 1080 }], null, 2));
              }}
            >
              {t("network.obAdd")}
            </Button>
            <Button variant="glass" onClick={() => setShowImport(true)}>
              {t("network.obImport")}
            </Button>
          </div>
          <Field label={t("network.outbounds")} error={errors.outbounds_override}>
            <textarea className="input mono" style={{ minHeight: 140 }} value={text} onChange={(e) => setText(e.target.value)} spellCheck={false} wrap="off" />
          </Field>
        </>
      ) : (
        <>
          {list.length === 0 ? (
            <p className="mb-3 text-[13px] text-[var(--ink-500)]">{t("network.empty")}</p>
          ) : (
            <ul className="row-list mb-3">
              {list.map((o) => (
                <OutboundRow key={o.name} o={o} probe={probes[o.name]} onProbe={() => probe(o.name)} />
              ))}
            </ul>
          )}
          <div className="mb-3 flex flex-wrap gap-2">
            <Button variant="glass" onClick={() => setShowImport(true)}>
              {t("network.obImport")}
            </Button>
          </div>
        </>
      )}
      {errors.outbounds_override ? (
        <p className="mb-3 text-xs text-[var(--berry-600)]" role="alert">
          {errors.outbounds_override}
        </p>
      ) : null}
      <Button type="submit" variant="primary" loading={save.isPending}>
        {t("network.save")}
      </Button>
      <OutboundAddModal
        open={showAdd}
        onClose={() => setShowAdd(false)}
        onAdd={(name, json) => {
          setText(JSON.stringify([...parseOutbounds(text), { name, ...JSON.parse(json) }], null, 2));
          setShowAdd(false);
        }}
      />
      <OutboundImportModal
        open={showImport}
        onClose={() => setShowImport(false)}
        onImport={(name, conf) =>
          unwrap(api.POST("/api/v1/nodes/{id}/outbounds/import", { params: { path: { id: nodeId } }, body: { name, conf } })).then((v) => {
            void qc.invalidateQueries({ queryKey: ["node-network", nodeId] });
            toast.ok(t("settings.saved"));
            setText(v.outbounds_override);
          })
        }
      />
      <DeleteConfirm open={confirm != null} onClose={() => setConfirm(null)} onConfirm={() => confirm && remove(confirm)} name={confirm ?? ""} />
    </form>
  );
}

function effectiveOutbounds(n: NodeNet): OutboundDoc[] {
  return (n.effective_outbounds ?? []).map((o) => {
    let doc: OutboundDoc = { name: o.name };
    try {
      const raw = typeof o.config === "string" ? o.config : JSON.stringify(o.config ?? {});
      doc = { name: o.name, ...((JSON.parse(raw || "{}") ?? {}) as Record<string, unknown>) };
    } catch {
      // keep the name
    }
    return doc;
  });
}

function OutboundRow({
  o,
  probe,
  onProbe,
  onDelete,
}: {
  o: OutboundDoc;
  probe?: { busy: boolean; res?: Probe; err?: string };
  onProbe: () => void;
  onDelete?: () => void;
}) {
  return (
    <li className="flex flex-wrap items-center justify-between gap-3 py-3">
      <div className="min-w-0">
        <div className="text-[13px] font-semibold">{o.name}</div>
        <div className="text-xs text-[var(--ink-500)]">{[o.type, o.server, o.port].filter(Boolean).join(" · ")}</div>
        {probe?.res ? (
          <div className="mt-1 text-xs">
            <Pill tone={probe.res.ok ? "ok" : "bad"}>
              {probe.res.ok ? `${t("network.probeOk")} · ${probe.res.ip || ""}` : `${t("network.probeFail")} · ${probe.res.error || ""}`}
            </Pill>
          </div>
        ) : probe?.err ? (
          <div className="mt-1 text-xs text-[var(--berry-600)]">{probe.err}</div>
        ) : null}
      </div>
      <div className="flex gap-1">
        <Button variant="glass" loading={probe?.busy} onClick={onProbe}>
          {t("network.probe")}
        </Button>
        {onDelete ? (
          <Button variant="ghost" onClick={onDelete}>
            {t("network.obDelete")}
          </Button>
        ) : null}
      </div>
    </li>
  );
}

function NodeTester({ nodeId }: { nodeId: number }) {
  const [domain, setDomain] = useState("");
  const [ip, setIp] = useState("");
  const [port, setPort] = useState("443");
  const [network, setNetwork] = useState<"tcp" | "udp">("tcp");
  const [inbound, setInbound] = useState("");
  const [res, setRes] = useState<Schemas["RouteTestOutputBody"] | null>(null);
  const [error, setError] = useState("");
  const [busy, setBusy] = useState(false);
  const run = () => {
    setBusy(true);
    setError("");
    setRes(null);
    void unwrap(
      api.POST("/api/v1/nodes/{id}/route-test", {
        params: { path: { id: nodeId } },
        body: { domain: domain.trim() || undefined, ip: ip.trim() || undefined, port: Number(port) || undefined, network: network || undefined, inbound: inbound.trim() || undefined },
      }),
    ).then(
      (v) => {
        setRes(v);
        setBusy(false);
      },
      (e: unknown) => {
        setError(errorText(e));
        setBusy(false);
      },
    );
  };
  return (
    <div>
      <p className="mb-3 text-[13px] text-[var(--ink-500)]">{t("network.testerSub")}</p>
      <div className="grid gap-3 sm:grid-cols-2">
        <Field label={t("network.tDomain")}>
          <input className="input" value={domain} onChange={(e) => setDomain(e.target.value)} placeholder="example.com" />
        </Field>
        <Field label={t("network.tIp")}>
          <input className="input" value={ip} onChange={(e) => setIp(e.target.value)} placeholder="203.0.113.9" />
        </Field>
        <Field label={t("network.tPort")}>
          <input className="input" inputMode="numeric" value={port} onChange={(e) => setPort(e.target.value)} placeholder="443" />
        </Field>
        <Field label={t("network.tNetwork")}>
          <div className="flex gap-2">
            {(["tcp", "udp"] as const).map((x) => (
              <button key={x} type="button" className="chip-btn" aria-pressed={network === x} onClick={() => setNetwork(x)}>
                {x.toUpperCase()}
              </button>
            ))}
          </div>
        </Field>
      </div>
      <Field label={t("network.tInbound")}>
        <input className="input" value={inbound} onChange={(e) => setInbound(e.target.value)} placeholder="" />
      </Field>
      <Button variant="primary" disabled={(!domain.trim() && !ip.trim()) || busy} loading={busy} onClick={run}>
        {t("network.tRun")}
      </Button>
      {error ? (
        <p className="mt-3 text-xs text-[var(--berry-600)]" role="alert">
          {error}
        </p>
      ) : null}
      {res ? (
        <div className="mt-3 flex flex-col gap-2">
          <MatchCard title={t("network.tDry")} m={res.dry} />
          {res.live ? (
            <MatchCard title={t("network.tLive")} m={res.live} />
          ) : (
            <p className="text-xs text-[var(--ink-500)]">{t("network.tOffline")}</p>
          )}
        </div>
      ) : null}
    </div>
  );
}

function MatchCard({ title, m }: { title: string; m: { matched: boolean; rule?: string; target?: string; geo_skipped?: boolean } }) {
  return (
    <div className="panel-soft p-3">
      <div className="mb-2 flex items-center gap-2 text-[13px] font-semibold">
        {title}
        {m.matched ? <Pill tone="ok">{t("network.tMatched")}</Pill> : <Pill tone="off">{t("network.tNoMatch")}</Pill>}
      </div>
      {m.matched ? (
        <div className="text-xs text-[var(--ink-600)]">
          <div>
            {t("network.tTarget")}: <b className="mono">{m.target}</b>
          </div>
          <div className="mono mt-1 break-all">{m.rule}</div>
        </div>
      ) : null}
      {m.geo_skipped ? <div className="mt-1 text-xs text-[var(--honey-600)]">{t("network.tGeo")}</div> : null}
    </div>
  );
}

function NodeGeo({ nodeId }: { nodeId: number }) {
  const qc = useQueryClient();
  const toast = useToast();
  const geo = useQuery({
    queryKey: ["node-geo", nodeId],
    queryFn: ({ signal }) => unwrap(api.GET("/api/v1/nodes/{id}/geo", { params: { path: { id: nodeId } }, signal })),
  });
  const update = useMutation({
    mutationFn: () => unwrap(api.POST("/api/v1/nodes/{id}/geo/update", { params: { path: { id: nodeId } } })),
    onSuccess: (data) => {
      qc.setQueryData(["node-geo", nodeId], data);
      toast.ok(t("settings.saved"));
    },
    onError: (e) => toast.error(errorText(e)),
  });
  return (
    <QueryBoundary query={geo} pending={<Skeleton style={{ height: 120, borderRadius: 16 }} />}>
      {(g) => (
        <>
          <p className="mb-3 text-[13px] text-[var(--ink-500)]">{t("network.geoSub")}</p>
          {(g.files ?? []).length === 0 ? (
            <p className="mb-3 text-[13px] text-[var(--ink-500)]">{t("network.empty")}</p>
          ) : (
            <ul className="row-list mb-3">
              {(g.files ?? []).map((f) => (
                <li key={f.name} className="flex flex-wrap items-center justify-between gap-3 py-3">
                  <div className="min-w-0">
                    <div className="text-[13px] font-semibold">{f.name}</div>
                    <div className="text-xs text-[var(--ink-500)]">
                      {f.present ? `${Math.round((f.size ?? 0) / 1024)} KB` : t("network.geoMissing")}
                    </div>
                  </div>
                  {f.present ? <Pill tone="ok">{t("network.geoOk")}</Pill> : <Pill tone="off">{t("network.geoMissing")}</Pill>}
                </li>
              ))}
            </ul>
          )}
          <Button variant="primary" loading={update.isPending} onClick={() => update.mutate()}>
            {t("network.geoUpdate")}
          </Button>
        </>
      )}
    </QueryBoundary>
  );
}
