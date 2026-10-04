// Network: the global DNS, routes and outbounds every node inherits, plus the
// subscription DNS client profiles carry. Nodes override these on their own card.
import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { useEffect, useState, type FormEvent } from "react";
import { api, ApiError, errorText, unwrap, type Schemas } from "../../api/client";
import { useNodes } from "../../api/hooks";
import { Confirm, Drawer } from "../../components/overlay";
import { QueryBoundary } from "../../components/query";
import { Switch } from "../../components/switch";
import { Tabs } from "../../components/tabs";
import { useToast } from "../../components/toast";
import { Button, Field, PageHeader, Skeleton } from "../../components/ui";
import { t } from "../../i18n";
import { fieldErrors } from "../../lib/fields";
import { NodeNetworkSections } from "./node-network";

export type DnsServerRow = { address: string; port: string; tag: string; domains: string };

export type DnsDoc = {
  enable: boolean;
  ipv6?: boolean;
  prefer_h3?: boolean;
  use_system_hosts?: boolean;
  servers?: DnsServerRow[];
  proxy_servers?: string[];
  fallback?: string[];
  fallback_filter?: { geoip?: string[]; geosite?: string[]; ipcidr?: string[]; domain?: string[] };
  hosts?: Record<string, string[]>;
};

export type SubDnsDoc = {
  servers?: DnsServerRow[];
  proxy_servers?: string[];
  fallback?: string[];
  fallback_filter?: { geoip?: string[]; geosite?: string[]; ipcidr?: string[]; domain?: string[] };
  fake_ip?: boolean;
  ipv6?: boolean;
};

export type DnsFilterText = { geoip: string; geosite: string; ipcidr: string; domain: string };

export type OutboundDoc = { name: string; type?: string; server?: string; port?: number; [k: string]: unknown };

// Server presets: Cloudflare, Google, Quad9, OpenDNS and xbox-dns.ru.
export const DNS_PRESETS: { id: string; label: string; servers: { address: string; domains?: string[] }[] }[] = [
  { id: "cloudflare", label: "Cloudflare", servers: [{ address: "1.1.1.1" }, { address: "1.0.0.1" }, { address: "https://1.1.1.1/dns-query" }] },
  { id: "google", label: "Google", servers: [{ address: "8.8.8.8" }, { address: "8.8.4.4" }, { address: "https://dns.google/dns-query" }] },
  { id: "quad9", label: "Quad9", servers: [{ address: "9.9.9.9" }, { address: "https://dns.quad9.net/dns-query" }] },
  { id: "opendns", label: "OpenDNS", servers: [{ address: "208.67.222.222" }, { address: "https://doh.opendns.com/dns-query" }] },
  // xbox-dns.ru answers for Google: the matcher scopes it to google domains,
  // everything else keeps going to the default servers.
  { id: "xbox", label: "xbox-dns", servers: [{ address: "https://xbox-dns.ru/dns-query", domains: ["geosite:google"] }] },
];

const TABS = ["dns", "routes", "outbounds", "subdns", "adblock"] as const;
type Tab = (typeof TABS)[number];

function useNetwork() {
  return useQuery({ queryKey: ["network"], queryFn: ({ signal }) => unwrap(api.GET("/api/v1/network", { signal })) });
}

function parseJson<T>(text: string, fallback: T): T {
  try {
    const v = JSON.parse(text || "null") as T;
    return v ?? fallback;
  } catch {
    return fallback;
  }
}

export function NetworkPage() {
  const [tab, setTab] = useState<Tab>("dns");
  const net = useNetwork();
  const nodes = useNodes();
  const [nodeId, setNodeId] = useState<number | null>(null);
  const picked = (nodes.data ?? []).find((n) => n.id === nodeId) ?? (nodes.data ?? [])[0];
  return (
    <>
      <PageHeader title={t("nav.network")} sub={t("network.sub")} />
      <Tabs
        id="network"
        label={t("nav.network")}
        tabs={TABS.map((id) => ({ id, label: t(`network.${id}`) }))}
        value={tab}
        onChange={setTab}
      >
        <QueryBoundary query={net} pending={<Skeleton style={{ height: 320, borderRadius: 20 }} />}>
          {(n) =>
            tab === "dns" ? (
              <GlobalDNSCard key="dns" doc={n.node_dns} />
            ) : tab === "routes" ? (
              <GlobalRoutesCard key="routes" doc={n.node_routes} />
            ) : tab === "outbounds" ? (
              <GlobalOutboundsCard key="outbounds" doc={n.node_outbounds} />
            ) : tab === "subdns" ? (
              <SubDNSCard key="subdns" doc={n.sub_dns} />
            ) : (
              <AdBlockCard key="adblock" nodeDoc={n.node_adblock} subDoc={n.sub_adblock} />
            )
          }
        </QueryBoundary>
      </Tabs>
      <section className="card glass mt-4 max-w-4xl">
        <div className="card-head">
          <div>
            <h2 className="card-title">{t("nav.nodes")}</h2>
            <div className="card-sub">{t("network.nodeHint")}</div>
          </div>
          <select
            className="input max-w-[240px]"
            aria-label={t("nav.nodes")}
            value={picked?.id ?? ""}
            onChange={(e) => setNodeId(Number(e.target.value) || null)}
          >
            {(nodes.data ?? []).map((n) => (
              <option key={n.id} value={n.id}>
                {n.name || `#${n.id}`}
              </option>
            ))}
          </select>
        </div>
        {picked ? <NodeNetworkSections key={picked.id} nodeId={picked.id} /> : null}
      </section>
    </>
  );
}

function useSaveNetwork() {
  const qc = useQueryClient();
  const toast = useToast();
  return useMutation({
    mutationFn: (body: Schemas["PatchNetworkInputBody"]) => unwrap(api.PATCH("/api/v1/network", { body })),
    onSuccess: (data) => {
      qc.setQueryData(["network"], data);
      toast.ok(t("settings.saved"));
    },
    onError: (e) => {
      if (!(e instanceof ApiError && Object.keys(e.fields).length)) toast.error(errorText(e));
    },
  });
}

function lines(v: string[] | undefined): string {
  return (v ?? []).join("\n");
}
function unlines(s: string): string[] {
  return s
    .split("\n")
    .map((x) => x.trim())
    .filter(Boolean);
}

export type KVRow = { k: string; v: string };

function kvRows(obj: Record<string, string[]> | undefined): KVRow[] {
  return Object.entries(obj ?? {}).map(([k, v]) => ({ k, v: v.join(", ") }));
}

function KVEditor({
  rows,
  onRows,
  keyLabel,
  keyPlaceholder,
  valuePlaceholder,
}: {
  rows: KVRow[];
  onRows: (r: KVRow[]) => void;
  keyLabel: string;
  keyPlaceholder: string;
  valuePlaceholder: string;
}) {
  return (
    <>
      {rows.map((r, i) => (
        <div key={i} className="mb-2 flex gap-2">
          <input
            className="input max-w-[220px]"
            aria-label={keyLabel}
            value={r.k}
            onChange={(e) => onRows(rows.map((x, j) => (j === i ? { ...x, k: e.target.value } : x)))}
            placeholder={keyPlaceholder}
          />
          <input
            className="input"
            value={r.v}
            onChange={(e) => onRows(rows.map((x, j) => (j === i ? { ...x, v: e.target.value } : x)))}
            placeholder={valuePlaceholder}
          />
          <Button variant="ghost" onClick={() => onRows(rows.filter((_, j) => j !== i))}>
            {t("network.delete")}
          </Button>
        </div>
      ))}
      <Button variant="glass" onClick={() => onRows([...rows, { k: "", v: "" }])}>
        {t("network.add")}
      </Button>
    </>
  );
}

export function kvObject(rows: KVRow[]): Record<string, string[]> {
  const out: Record<string, string[]> = {};
  for (const r of rows) {
    if (!r.k.trim()) continue;
    out[r.k.trim()] = r.v.split(/[,\s]+/).filter(Boolean);
  }
  return out;
}

export function DNSEditor({
  form,
  onForm,
  hostRows,
  onHostRows,
  showEnable = true,
}: {
  form: DnsDoc;
  onForm: (k: keyof DnsDoc, v: DnsDoc[keyof DnsDoc]) => void;
  hostRows: KVRow[];
  onHostRows: (r: KVRow[]) => void;
  showEnable?: boolean;
}) {
  const servers = form.servers ?? [];
  const setServers = (v: DnsServerRow[]) => onForm("servers", v);
  const toast = useToast();
  const filter = form.fallback_filter ?? {};
  const setFilter = (k: keyof NonNullable<DnsDoc["fallback_filter"]>, v: string) =>
    onForm("fallback_filter", { ...filter, [k]: v.split(/[,\s]+/).filter(Boolean) });
  const addPreset = (id: string) => {
    const p = DNS_PRESETS.find((x) => x.id === id);
    if (!p) return;
    const have = new Set(servers.map((s) => s.address));
    const fresh = p.servers.filter((s) => !have.has(s.address));
    if (fresh.length === 0) {
      toast.ok(t("network.presetExists"));
      return;
    }
    const next = [...servers];
    for (const s of fresh) next.push({ address: s.address, port: "", tag: "", domains: (s.domains ?? []).join(", ") });
    setServers(next);
    toast.ok(t("network.presetAdded", { n: fresh.length }));
  };
  return (
    <>
      {showEnable ? (
        <div className="mb-4 flex items-center justify-between gap-3">
          <div>
            <div className="text-[13px] font-semibold">{t("network.enable")}</div>
            <div className="text-xs text-[var(--ink-500)]">{t("network.enableHint")}</div>
          </div>
          <Switch checked={form.enable} onChange={(v) => onForm("enable", v)} label={t("network.enable")} />
        </div>
      ) : null}
      {(!showEnable || form.enable) && (
        <>
          {servers.map((s, i) => (
            <div key={i} className="panel-soft mb-2 p-3">
              <div className="mb-2 flex gap-2">
                <input
                  className="input mono"
                  aria-label={t("network.serverAddress")}
                  value={s.address}
                  onChange={(e) => setServers(servers.map((x, j) => (j === i ? { ...x, address: e.target.value } : x)))}
                  placeholder="1.1.1.1 / https://…"
                  spellCheck={false}
                />
                <input
                  className="input max-w-[90px]"
                  aria-label={t("network.serverPort")}
                  value={s.port}
                  inputMode="numeric"
                  onChange={(e) => setServers(servers.map((x, j) => (j === i ? { ...x, port: e.target.value } : x)))}
                  placeholder="53"
                />
                <Button variant="ghost" onClick={() => setServers(servers.filter((_, j) => j !== i))}>
                  {t("network.delete")}
                </Button>
              </div>
              <div className="flex gap-2">
                <input
                  className="input max-w-[160px]"
                  aria-label={t("network.serverTag")}
                  value={s.tag}
                  onChange={(e) => setServers(servers.map((x, j) => (j === i ? { ...x, tag: e.target.value } : x)))}
                  placeholder="xbox"
                  maxLength={32}
                />
                <input
                  className="input"
                  aria-label={t("network.serverDomains")}
                  value={s.domains}
                  onChange={(e) => setServers(servers.map((x, j) => (j === i ? { ...x, domains: e.target.value } : x)))}
                  placeholder="geosite:google"
                  spellCheck={false}
                />
              </div>
            </div>
          ))}
          <div className="mb-3 flex flex-wrap items-center gap-2">
            <Button variant="glass" onClick={() => setServers([...servers, { address: "", port: "", tag: "", domains: "" }])}>
              {t("network.add")}
            </Button>
            {DNS_PRESETS.map((p) => (
              <button key={p.id} type="button" className="chip-btn" title={p.servers.map((s) => s.address).join(", ")} onClick={() => addPreset(p.id)}>
                + {p.label}
              </button>
            ))}
          </div>
          <p className="mb-3 text-xs text-[var(--ink-500)]">{t("network.serverDomainsHint")}</p>
          <Field label={t("network.proxy")} hint={t("network.proxyHint")}>
            <textarea
              className="input"
              style={{ minHeight: 56 }}
              value={lines(form.proxy_servers)}
              onChange={(e) => onForm("proxy_servers", unlines(e.target.value))}
              spellCheck={false}
            />
          </Field>
          <Field label={t("network.fallback")} hint={t("network.fallbackHint")}>
            <textarea
              className="input"
              style={{ minHeight: 56 }}
              value={lines(form.fallback)}
              onChange={(e) => onForm("fallback", unlines(e.target.value))}
              placeholder={"8.8.8.8\nhttps://dns.google/dns-query"}
              spellCheck={false}
            />
          </Field>
          <Field label={t("network.filterGeoip")} hint={t("network.filterHint")}>
            <div className="grid gap-2 sm:grid-cols-2">
              <input
                className="input"
                aria-label={t("network.filterGeoip")}
                value={(filter.geoip ?? []).join(", ")}
                onChange={(e) => setFilter("geoip", e.target.value)}
                placeholder="cn"
              />
              <input
                className="input"
                aria-label={t("network.filterGeosite")}
                value={(filter.geosite ?? []).join(", ")}
                onChange={(e) => setFilter("geosite", e.target.value)}
                placeholder="geosite"
              />
              <input
                className="input"
                aria-label={t("network.filterIpcidr")}
                value={(filter.ipcidr ?? []).join(", ")}
                onChange={(e) => setFilter("ipcidr", e.target.value)}
                placeholder="10.0.0.0/8"
              />
              <input
                className="input"
                aria-label={t("network.filterDomain")}
                value={(filter.domain ?? []).join(", ")}
                onChange={(e) => setFilter("domain", e.target.value)}
                placeholder="example.com"
              />
            </div>
          </Field>
          <div className="mb-4 flex flex-wrap gap-4">
            <label className="flex items-center gap-2 text-[13px]">
              <input type="checkbox" className="check" checked={!!form.ipv6} onChange={(e) => onForm("ipv6", e.target.checked)} /> IPv6
            </label>
            <label className="flex items-center gap-2 text-[13px]">
              <input type="checkbox" className="check" checked={!!form.prefer_h3} onChange={(e) => onForm("prefer_h3", e.target.checked)} /> {t("network.preferH3")}
            </label>
            <label className="flex items-center gap-2 text-[13px]">
              <input type="checkbox" className="check" checked={!!form.use_system_hosts} onChange={(e) => onForm("use_system_hosts", e.target.checked)} /> {t("network.useSystemHosts")}
            </label>
          </div>
          <Field label={t("network.hosts")} hint={t("network.hostsHint")}>
            <KVEditor rows={hostRows} onRows={onHostRows} keyLabel={t("network.hostDomain")} keyPlaceholder="internal.example" valuePlaceholder="10.1.2.3" />
          </Field>
        </>
      )}
    </>
  );
}

function serversToJson(servers: DnsServerRow[]): { address: string; port?: number; domains?: string[]; tag?: string }[] {
  return servers
    .filter((s) => s.address.trim())
    .map((s) => {
      const o: { address: string; port?: number; domains?: string[]; tag?: string } = { address: s.address.trim() };
      const port = Number(s.port);
      if (s.port.trim() && Number.isInteger(port) && port > 0) o.port = port;
      const domains = s.domains
        .split(/[,\s]+/)
        .map((x) => x.trim())
        .filter(Boolean);
      if (domains.length) o.domains = domains;
      if (s.tag.trim()) o.tag = s.tag.trim();
      return o;
    });
}

export function dnsToJson(form: DnsDoc, hostRows: KVRow[]): string {
  const out: Record<string, unknown> = { enable: form.enable };
  if (form.enable) {
    if (form.ipv6) out.ipv6 = true;
    if (form.prefer_h3) out.prefer_h3 = true;
    if (form.use_system_hosts) out.use_system_hosts = true;
    out.servers = serversToJson(form.servers ?? []);
    const proxy = unlines(lines(form.proxy_servers));
    if (proxy.length) out.proxy_servers = proxy;
    const fallback = unlines(lines(form.fallback));
    if (fallback.length) out.fallback = fallback;
    const f = form.fallback_filter ?? {};
    const filter: Record<string, string[]> = {};
    for (const k of ["geoip", "geosite", "ipcidr", "domain"] as const) {
      const v = (f[k] ?? []).filter(Boolean);
      if (v.length) filter[k] = v;
    }
    if (Object.keys(filter).length) out.fallback_filter = filter;
    const hosts = kvObject(hostRows);
    if (Object.keys(hosts).length) out.hosts = hosts;
  }
  return JSON.stringify(out);
}

export function parseDnsDoc(doc: string): { form: DnsDoc; hostRows: KVRow[] } {
  const parsed = parseJson<{
    enable?: boolean;
    ipv6?: boolean;
    prefer_h3?: boolean;
    use_system_hosts?: boolean;
    servers?: { address?: string; port?: number; domains?: string[]; tag?: string }[];
    nameservers?: string[];
    proxy_servers?: string[];
    policy?: Record<string, string[]>;
    fallback?: string[];
    fallback_filter?: DnsDoc["fallback_filter"];
    hosts?: Record<string, string[]>;
  }>(doc, {});
  const servers: DnsServerRow[] = (parsed.servers ?? []).map((s) => ({
    address: s.address ?? "",
    port: s.port ? String(s.port) : "",
    tag: s.tag ?? "",
    domains: (s.domains ?? []).join(", "),
  }));
  // Legacy flat shape converts on load: plain servers stay default, policy keys
  // attach to the servers named by their values.
  if (!parsed.servers) {
    for (const a of parsed.nameservers ?? []) servers.push({ address: a, port: "", tag: "", domains: "" });
    const at = new Map(servers.map((s, i) => [s.address, i]));
    for (const [key, vs] of Object.entries(parsed.policy ?? {})) {
      for (const a of vs) {
        const i = at.get(a);
        if (i == null) {
          at.set(a, servers.length);
          servers.push({ address: a, port: "", tag: "", domains: key });
        } else if (i != null) {
          const cur = servers[i]!.domains;
          servers[i]!.domains = cur ? `${cur}, ${key}` : key;
        }
      }
    }
  }
  const form: DnsDoc = {
    enable: parsed.enable ?? false,
    ipv6: parsed.ipv6,
    prefer_h3: parsed.prefer_h3,
    use_system_hosts: parsed.use_system_hosts,
    servers,
    proxy_servers: parsed.proxy_servers,
    fallback: parsed.fallback,
    fallback_filter: parsed.fallback_filter,
    hosts: parsed.hosts,
  };
  return { form, hostRows: kvRows(form.hosts) };
}

export function RoutesEditor({
  def,
  onDef,
  text,
  onText,
  targets,
  error,
}: {
  def: string;
  onDef: (v: string) => void;
  text: string;
  onText: (v: string) => void;
  targets: string[];
  error?: string;
}) {
  return (
    <>
      <Field label={t("network.default")} hint={t("network.defaultHint")}>
        <div className="flex gap-2">
          <input className="input max-w-[220px]" value={def} onChange={(e) => onDef(e.target.value)} list="route-targets" />
          <datalist id="route-targets">
            {targets.map((x) => (
              <option key={x} value={x} />
            ))}
          </datalist>
        </div>
      </Field>
      <Field label={t("network.rules")} error={error}>
        <textarea
          className="input mono"
          style={{ minHeight: 220 }}
          value={text}
          onChange={(e) => onText(e.target.value)}
          placeholder={t("network.rulesPlaceholder")}
          spellCheck={false}
          wrap="off"
        />
      </Field>
    </>
  );
}

export function splitDefault(text: string): { def: string; rules: string } {
  const m = text.match(/^# default:\s*(\S+)\s*$/m);
  return { def: m?.[1] ?? "", rules: text.replace(/^# default:\s*\S+\s*$/m, "").replace(/^\n+/, "") };
}

export function joinDefault(def: string, rules: string): string {
  const d = def.trim();
  const body = rules.replace(/\s+$/, "") + "\n";
  return d && d.toUpperCase() !== "DIRECT" ? `# default: ${d}\n${body}` : body;
}

export const ROUTE_TARGETS = ["DIRECT", "REJECT", "REJECT-DROP", "WARP"];

function GlobalRoutesCard({ doc }: { doc: string }) {
  const save = useSaveNetwork();
  const parsed = splitDefault(doc);
  const [def, setDef] = useState(parsed.def || "DIRECT");
  const [text, setText] = useState(parsed.rules);
  const errors = fieldErrors(save.error);
  const submit = (e: FormEvent) => {
    e.preventDefault();
    save.mutate({ node_routes: joinDefault(def, text) });
  };
  return (
    <section className="card glass max-w-4xl">
      <form onSubmit={submit} noValidate>
        <div className="card-head">
          <div>
            <h2 className="card-title">{t("network.routes")}</h2>
            <div className="card-sub">{t("network.rulesHint")}</div>
          </div>
        </div>
        <RoutesEditor def={def} onDef={setDef} text={text} onText={setText} targets={ROUTE_TARGETS} error={errors.node_routes} />
        <Button type="submit" variant="primary" loading={save.isPending}>
          {t("network.save")}
        </Button>
      </form>
    </section>
  );
}

function GlobalDNSCard({ doc }: { doc: string }) {
  const save = useSaveNetwork();
  const initial = parseDnsDoc(doc);
  const [form, setForm] = useState<DnsDoc>(initial.form);
  const [hostRows, setHostRows] = useState<KVRow[]>(initial.hostRows);
  const errors = fieldErrors(save.error);
  const submit = (e: FormEvent) => {
    e.preventDefault();
    save.mutate({ node_dns: dnsToJson(form, hostRows) });
  };
  const set = (k: keyof DnsDoc) => (v: DnsDoc[keyof DnsDoc]) => setForm((f) => ({ ...f, [k]: v }));
  return (
    <section className="card glass max-w-4xl">
      <form onSubmit={submit} noValidate>
        <div className="card-head">
          <div>
            <h2 className="card-title">{t("network.dns")}</h2>
          </div>
        </div>
        <DNSEditor form={form} onForm={set} hostRows={hostRows} onHostRows={setHostRows} />
        {errors.node_dns ? (
          <p className="mb-3 text-xs text-[var(--berry-600)]" role="alert">
            {errors.node_dns}
          </p>
        ) : null}
        <Button type="submit" variant="primary" loading={save.isPending}>
          {t("network.save")}
        </Button>
      </form>
    </section>
  );
}

export function parseOutbounds(text: string): OutboundDoc[] {
  try {
    const v = JSON.parse(text || "[]") as unknown;
    return Array.isArray(v) ? (v as OutboundDoc[]) : [];
  } catch {
    return [];
  }
}

function GlobalOutboundsCard({ doc }: { doc: string }) {
  const save = useSaveNetwork();
  const [text, setText] = useState(doc);
  const [showAdd, setShowAdd] = useState(false);
  const [showImport, setShowImport] = useState(false);
  const list = parseOutbounds(text);
  const errors = fieldErrors(save.error);
  const submit = (e: FormEvent) => {
    e.preventDefault();
    save.mutate({ node_outbounds: text });
  };
  const remove = (name: string) => {
    setText(JSON.stringify(list.filter((o) => o.name !== name), null, 2));
  };
  return (
    <section className="card glass max-w-4xl">
      <form onSubmit={submit} noValidate>
        <div className="card-head">
          <div>
            <h2 className="card-title">{t("network.outbounds")}</h2>
            <div className="card-sub">{t("network.outboundsHint")}</div>
          </div>
        </div>
        {list.length === 0 ? (
          <p className="mb-3 text-[13px] text-[var(--ink-500)]">{t("network.empty")}</p>
        ) : (
          <ul className="row-list mb-3">
            {list.map((o) => (
              <li key={o.name} className="flex flex-wrap items-center justify-between gap-3 py-3">
                <div className="min-w-0">
                  <div className="text-[13px] font-semibold">{o.name}</div>
                  <div className="text-xs text-[var(--ink-500)]">
                    {[o.type, o.server, o.port].filter(Boolean).join(" · ")}
                  </div>
                </div>
                <div className="flex gap-1">
                  <Button variant="ghost" onClick={() => remove(o.name)}>
                    {t("network.obDelete")}
                  </Button>
                </div>
              </li>
            ))}
          </ul>
        )}
        <div className="mb-3 flex flex-wrap gap-2">
          <Button variant="glass" onClick={() => setShowAdd(true)}>
            {t("network.obAdd")}
          </Button>
          <Button variant="glass" onClick={() => setShowImport(true)}>
            {t("network.obImport")}
          </Button>
        </div>
        <Field label={t("network.outbounds")} error={errors.node_outbounds}>
          <textarea
            className="input mono"
            style={{ minHeight: 140 }}
            value={text}
            onChange={(e) => setText(e.target.value)}
            placeholder='[{"name":"office","type":"wireguard",...}]'
            spellCheck={false}
            wrap="off"
          />
        </Field>
        <Button type="submit" variant="primary" loading={save.isPending}>
          {t("network.save")}
        </Button>
      </form>
      <OutboundAddModal
        open={showAdd}
        onClose={() => setShowAdd(false)}
        onAdd={(name, json) => {
          setText(JSON.stringify([...parseOutbounds(text), { name, ...parseJson<Record<string, unknown>>(json, {}) }], null, 2));
          setShowAdd(false);
        }}
      />
      <OutboundImportModal
        open={showImport}
        onClose={() => setShowImport(false)}
        onImport={(name, conf) => {
          setText(JSON.stringify([...parseOutbounds(text), parseWireGuardConf(name, conf)], null, 2));
          return Promise.resolve();
        }}
      />
    </section>
  );
}

export function OutboundAddModal({ open, onClose, onAdd }: { open: boolean; onClose: () => void; onAdd: (name: string, json: string) => void }) {
  const [name, setName] = useState("");
  const [json, setJson] = useState('{\n  "type": "socks5",\n  "server": "127.0.0.1",\n  "port": 1080\n}');
  useEffect(() => {
    if (open) {
      setName("");
      setJson('{\n  "type": "socks5",\n  "server": "127.0.0.1",\n  "port": 1080\n}');
    }
  }, [open ]);
  return (
    <Drawer open={open} onOpenChange={(v) => !v && onClose()} title={t("network.obAdd")}>
      <div className="pt-5">
        <Field label={t("network.obName")}>
          <input className="input" value={name} onChange={(e) => setName(e.target.value)} maxLength={64} />
        </Field>
        <Field label={t("network.obJson")}>
          <textarea className="input mono" style={{ minHeight: 160 }} value={json} onChange={(e) => setJson(e.target.value)} spellCheck={false} wrap="off" />
        </Field>
        <Button variant="primary" disabled={!name.trim()} onClick={() => onAdd(name.trim(), json)}>
          {t("network.obAdd")}
        </Button>
      </div>
    </Drawer>
  );
}

export function OutboundImportModal({ open, onClose, onImport }: { open: boolean; onClose: () => void; onImport: (name: string, conf: string) => Promise<void> }) {
  const [name, setName] = useState("");
  const [conf, setConf] = useState("");
  const [error, setError] = useState("");
  const [busy, setBusy] = useState(false);
  const reset = () => {
    setName("");
    setConf("");
    setError("");
    setBusy(false);
  };
  return (
    <Drawer
      open={open}
      onOpenChange={(v) => {
        if (!v) {
          reset();
          onClose();
        }
      }}
      title={t("network.obImportTitle")}
    >
      <div className="pt-5">
        <Field label={t("network.obImportName")}>
          <input className="input" value={name} onChange={(e) => setName(e.target.value)} maxLength={64} placeholder="nord" />
        </Field>
        <Field label={t("network.obImportConf")}>
          <textarea
            className="input mono"
            style={{ minHeight: 200 }}
            value={conf}
            onChange={(e) => setConf(e.target.value)}
            placeholder={"[Interface]\nPrivateKey = ...\n\n[Peer]\nPublicKey = ...\nEndpoint = ..."}
            spellCheck={false}
            wrap="off"
          />
        </Field>
        {error ? (
          <p className="mb-3 text-xs text-[var(--berry-600)]" role="alert">
            {error}
          </p>
        ) : null}
        <Button
          variant="primary"
          disabled={!name.trim() || !conf.trim() || busy}
          loading={busy}
          onClick={() => {
            setBusy(true);
            setError("");
            void onImport(name.trim(), conf).then(
              () => {
                reset();
                onClose();
              },
              (e: unknown) => {
                setError(errorText(e));
                setBusy(false);
              },
            );
          }}
        >
          {t("network.obImport")}
        </Button>
      </div>
    </Drawer>
  );
}

// parseWireGuardConf reads [Interface]/[Peer] into a mihomo wireguard proxy doc.
// The server revalidates on save; this only shapes the form.
export function parseWireGuardConf(name: string, conf: string): OutboundDoc {
  let section = "";
  const iface: Record<string, string> = {};
  const peer: Record<string, string> = {};
  for (const raw of conf.split("\n")) {
    const line = raw.trim();
    if (!line || line.startsWith("#") || line.startsWith(";")) continue;
    if (line.startsWith("[") && line.endsWith("]")) {
      section = line.slice(1, -1).toLowerCase();
      continue;
    }
    const eq = line.indexOf("=");
    if (eq < 0) continue;
    const k = line.slice(0, eq).trim().toLowerCase();
    const v = line.slice(eq + 1).trim();
    if (section === "interface") iface[k] = v;
    else if (section === "peer") peer[k] = v;
  }
  const endpoint = peer["endpoint"] ?? "";
  const colon = endpoint.lastIndexOf(":");
  if (!iface["privatekey"] || !peer["publickey"] || colon < 0) throw new Error("bad .conf");
  const server = endpoint.slice(0, colon);
  const port = Number(endpoint.slice(colon + 1));
  if (!server || !Number.isInteger(port)) throw new Error("bad .conf");
  const peers: Record<string, unknown> = {
    server,
    port,
    "public-key": peer["publickey"],
    "allowed-ips": (peer["allowedips"] || "0.0.0.0/0, ::/0").split(",").map((x) => x.trim()).filter(Boolean),
  };
  const doc: OutboundDoc = {
    name,
    type: "wireguard",
    server,
    port,
    "private-key": iface["privatekey"],
    peers: [peers],
  };
  const mtu = Number(iface["mtu"]);
  if (Number.isInteger(mtu) && mtu > 0) doc["mtu"] = mtu;
  return doc;
}

function SubDNSCard({ doc }: { doc: string }) {
  const save = useSaveNetwork();
  const parsed = parseJson<{
    servers?: { address?: string; port?: number; domains?: string[]; tag?: string }[];
    nameservers?: string[];
    proxy_servers?: string[];
    policy?: Record<string, string[]>;
    fallback?: string[];
    fallback_filter?: DnsDoc["fallback_filter"];
    fake_ip?: boolean;
    ipv6?: boolean;
  }>(doc, {});
  const toRows = (servers?: { address?: string; port?: number; domains?: string[]; tag?: string }[]): DnsServerRow[] =>
    (servers ?? []).map((s) => ({ address: s.address ?? "", port: s.port ? String(s.port) : "", tag: s.tag ?? "", domains: (s.domains ?? []).join(", ") }));
  const initial: DnsDoc & { fake_ip?: boolean } = {
    enable: true,
    ipv6: parsed.ipv6,
    servers: toRows(parsed.servers),
    proxy_servers: parsed.proxy_servers,
    fallback: parsed.fallback,
    fallback_filter: parsed.fallback_filter,
    fake_ip: parsed.fake_ip ?? true,
  };
  if (!parsed.servers) {
    for (const a of parsed.nameservers ?? []) initial.servers!.push({ address: a, port: "", tag: "", domains: "" });
    const at = new Map(initial.servers!.map((s, i) => [s.address, i]));
    for (const [key, vs] of Object.entries(parsed.policy ?? {})) {
      for (const a of vs) {
        const i = at.get(a);
        if (i == null) {
          at.set(a, initial.servers!.length);
          initial.servers!.push({ address: a, port: "", tag: "", domains: key });
        } else if (i != null) {
          const cur = initial.servers![i]!.domains;
          initial.servers![i]!.domains = cur ? `${cur}, ${key}` : key;
        }
      }
    }
  }
  const [form, setForm] = useState<DnsDoc & { fake_ip?: boolean }>(initial);
  const [hostRows] = useState<KVRow[]>([]);
  const errors = fieldErrors(save.error);
  const submit = (e: FormEvent) => {
    e.preventDefault();
    const out: Record<string, unknown> = { fake_ip: form.fake_ip ?? true };
    const servers = serversToJson(form.servers ?? []);
    if (servers.length) out.servers = servers;
    const proxy = unlines(lines(form.proxy_servers));
    if (proxy.length) out.proxy_servers = proxy;
    const fallback = unlines(lines(form.fallback));
    if (fallback.length) out.fallback = fallback;
    const f = form.fallback_filter ?? {};
    const filter: Record<string, string[]> = {};
    for (const k of ["geoip", "geosite", "ipcidr", "domain"] as const) {
      const v = (f[k] ?? []).filter(Boolean);
      if (v.length) filter[k] = v;
    }
    if (Object.keys(filter).length) out.fallback_filter = filter;
    if (form.ipv6) out.ipv6 = true;
    save.mutate({ sub_dns: JSON.stringify(out) });
  };
  const set = (k: keyof DnsDoc) => (v: DnsDoc[keyof DnsDoc]) => setForm((f) => ({ ...f, [k]: v }));
  return (
    <section className="card glass max-w-4xl">
      <form onSubmit={submit} noValidate>
        <div className="card-head">
          <div>
            <h2 className="card-title">{t("network.subdns")}</h2>
          </div>
        </div>
        <DNSEditor form={form} onForm={set} hostRows={hostRows} onHostRows={() => undefined} showEnable={false} />
        <div className="mb-4 flex flex-wrap gap-4">
          <label className="flex items-center gap-2 text-[13px]">
            <input
              type="checkbox"
              className="check"
              checked={form.fake_ip ?? true}
              onChange={(e) => setForm((f) => ({ ...f, fake_ip: e.target.checked }))}
            />{" "}
            {t("network.subFake")}
          </label>
        </div>
        {errors.sub_dns ? (
          <p className="mb-3 text-xs text-[var(--berry-600)]" role="alert">
            {errors.sub_dns}
          </p>
        ) : null}
        <Button type="submit" variant="primary" loading={save.isPending}>
          {t("network.save")}
        </Button>
      </form>
    </section>
  );
}

export function DeleteConfirm({ open, onClose, onConfirm, name }: { open: boolean; onClose: () => void; onConfirm: () => void; name: string }) {
  return (
    <Confirm open={open} onOpenChange={(v) => !v && onClose()} title={t("network.obDelete")} text={name} confirm={t("common.delete")} onConfirm={onConfirm} />
  );
}

export type AdBlockDoc = { enable: boolean; drop?: boolean; exceptions?: string[]; extra?: string[] };

function parseAdBlock(doc: string): AdBlockDoc {
  try {
    const v = JSON.parse(doc || "null") as Partial<AdBlockDoc> | null;
    return { enable: false, ...v } as AdBlockDoc;
  } catch {
    return { enable: false };
  }
}

function AdBlockSection({
  title,
  hint,
  form,
  onForm,
}: {
  title: string;
  hint: string;
  form: AdBlockDoc;
  onForm: (k: keyof AdBlockDoc, v: AdBlockDoc[keyof AdBlockDoc]) => void;
}) {
  return (
    <div className="mb-4">
      <div className="mb-3 flex items-center justify-between gap-3">
        <div>
          <div className="text-[13px] font-semibold">{title}</div>
          <div className="text-xs text-[var(--ink-500)]">{hint}</div>
        </div>
        <Switch checked={form.enable} onChange={(v) => onForm("enable", v)} label={title} />
      </div>
      {form.enable ? (
        <>
          <Field label={t("network.adMode")}>
            <div className="flex gap-2">
              {([false, true] as const).map((drop) => (
                <button
                  key={String(drop)}
                  type="button"
                  className="chip-btn"
                  aria-pressed={(form.drop ?? false) === drop}
                  onClick={() => onForm("drop", drop)}
                >
                  {drop ? t("network.adDrop") : t("network.adReject")}
                </button>
              ))}
            </div>
          </Field>
          <Field label={t("network.adExceptions")} hint={t("network.adExceptionsHint")}>
            <textarea
              className="input"
              style={{ minHeight: 56 }}
              value={(form.exceptions ?? []).join("\n")}
              onChange={(e) => onForm("exceptions", e.target.value.split("\n").map((x) => x.trim()).filter(Boolean))}
              placeholder="corp.example"
              spellCheck={false}
            />
          </Field>
          <Field label={t("network.adExtra")} hint={t("network.adExtraHint")}>
            <textarea
              className="input"
              style={{ minHeight: 56 }}
              value={(form.extra ?? []).join("\n")}
              onChange={(e) => onForm("extra", e.target.value.split("\n").map((x) => x.trim()).filter(Boolean))}
              placeholder="ads.example"
              spellCheck={false}
            />
          </Field>
        </>
      ) : null}
    </div>
  );
}

function adBlockJson(form: AdBlockDoc): string {
  const out: AdBlockDoc = { enable: form.enable };
  if (form.enable) {
    if (form.drop) out.drop = true;
    if ((form.exceptions ?? []).length) out.exceptions = form.exceptions;
    if ((form.extra ?? []).length) out.extra = form.extra;
  }
  return JSON.stringify(out);
}

function AdBlockCard({ nodeDoc, subDoc }: { nodeDoc: string; subDoc: string }) {
  const save = useSaveNetwork();
  const [node, setNode] = useState<AdBlockDoc>(() => parseAdBlock(nodeDoc));
  const [sub, setSub] = useState<AdBlockDoc>(() => parseAdBlock(subDoc));
  const errors = fieldErrors(save.error);
  const submit = (e: FormEvent) => {
    e.preventDefault();
    save.mutate({ node_adblock: adBlockJson(node), sub_adblock: adBlockJson(sub) });
  };
  const setN = (k: keyof AdBlockDoc) => (v: AdBlockDoc[keyof AdBlockDoc]) => setNode((f) => ({ ...f, [k]: v }));
  const setS = (k: keyof AdBlockDoc) => (v: AdBlockDoc[keyof AdBlockDoc]) => setSub((f) => ({ ...f, [k]: v }));
  return (
    <section className="card glass max-w-4xl">
      <form onSubmit={submit} noValidate>
        <div className="card-head">
          <div>
            <h2 className="card-title">{t("network.adblock")}</h2>
            <div className="card-sub">{t("network.adblockSub")}</div>
          </div>
        </div>
        <AdBlockSection title={t("network.adNode")} hint={t("network.adNodeHint")} form={node} onForm={setN} />
        <div className="border-t border-[var(--hairline)] pt-4">
          <AdBlockSection title={t("network.adSub")} hint={t("network.adSubHint")} form={sub} onForm={setS} />
        </div>
        {errors.node_adblock || errors.sub_adblock ? (
          <p className="mb-3 text-xs text-[var(--berry-600)]" role="alert">
            {errors.node_adblock ?? errors.sub_adblock}
          </p>
        ) : null}
        <Button type="submit" variant="primary" loading={save.isPending}>
          {t("network.save")}
        </Button>
      </form>
    </section>
  );
}
