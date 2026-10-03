// Network: the global DNS, routes and outbounds every node inherits, plus the
// subscription DNS client profiles carry. Nodes override these on their own card.
import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { useEffect, useState, type FormEvent } from "react";
import { api, ApiError, errorText, unwrap, type Schemas } from "../../api/client";
import { Confirm, Drawer } from "../../components/overlay";
import { QueryBoundary } from "../../components/query";
import { Switch } from "../../components/switch";
import { Tabs } from "../../components/tabs";
import { useToast } from "../../components/toast";
import { Button, Field, PageHeader, Skeleton } from "../../components/ui";
import { t } from "../../i18n";
import { fieldErrors } from "../../lib/fields";

export type DnsDoc = {
  enable: boolean;
  ipv6?: boolean;
  nameservers?: string[];
  proxy_servers?: string[];
  policy?: Record<string, string[]>;
  hosts?: Record<string, string[]>;
};

export type SubDnsDoc = {
  nameservers?: string[];
  proxy_servers?: string[];
  policy?: Record<string, string[]>;
  fake_ip?: boolean;
  ipv6?: boolean;
};

export type OutboundDoc = { name: string; type?: string; server?: string; port?: number; [k: string]: unknown };

const TABS = ["dns", "routes", "outbounds", "subdns"] as const;
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
            ) : (
              <SubDNSCard key="subdns" doc={n.sub_dns} />
            )
          }
        </QueryBoundary>
      </Tabs>
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
  polRows,
  onPolRows,
  hostRows,
  onHostRows,
}: {
  form: DnsDoc;
  onForm: (k: keyof DnsDoc, v: DnsDoc[keyof DnsDoc]) => void;
  polRows: KVRow[];
  onPolRows: (r: KVRow[]) => void;
  hostRows: KVRow[];
  onHostRows: (r: KVRow[]) => void;
}) {
  return (
    <>
      <div className="mb-4 flex items-center justify-between gap-3">
        <div>
          <div className="text-[13px] font-semibold">{t("network.enable")}</div>
          <div className="text-xs text-[var(--ink-500)]">{t("network.enableHint")}</div>
        </div>
        <Switch checked={form.enable} onChange={(v) => onForm("enable", v)} label={t("network.enable")} />
      </div>
      {form.enable ? (
        <>
          <Field label={t("network.servers")} hint={t("network.serversHint")}>
            <textarea
              className="input"
              style={{ minHeight: 80 }}
              value={lines(form.nameservers)}
              onChange={(e) => onForm("nameservers", unlines(e.target.value))}
              placeholder={"1.1.1.1\nhttps://dns.google/dns-query"}
              spellCheck={false}
            />
          </Field>
          <Field label={t("network.proxy")} hint={t("network.proxyHint")}>
            <textarea
              className="input"
              style={{ minHeight: 56 }}
              value={lines(form.proxy_servers)}
              onChange={(e) => onForm("proxy_servers", unlines(e.target.value))}
              spellCheck={false}
            />
          </Field>
          <Field label={t("network.policy")} hint={t("network.policyHint")}>
            <KVEditor rows={polRows} onRows={onPolRows} keyLabel={t("network.policyKey")} keyPlaceholder="geosite:cn" valuePlaceholder="223.5.5.5" />
          </Field>
          <Field label={t("network.hosts")} hint={t("network.hostsHint")}>
            <KVEditor rows={hostRows} onRows={onHostRows} keyLabel={t("network.hostDomain")} keyPlaceholder="internal.example" valuePlaceholder="10.1.2.3" />
          </Field>
        </>
      ) : null}
    </>
  );
}

export function dnsToJson(form: DnsDoc, polRows: KVRow[], hostRows: KVRow[]): string {
  const out: DnsDoc = { enable: form.enable };
  if (form.enable) {
    out.ipv6 = form.ipv6;
    out.nameservers = unlines(lines(form.nameservers));
    const proxy = unlines(lines(form.proxy_servers));
    if (proxy.length) out.proxy_servers = proxy;
    const policy = kvObject(polRows);
    if (Object.keys(policy).length) out.policy = policy;
    const hosts = kvObject(hostRows);
    if (Object.keys(hosts).length) out.hosts = hosts;
  }
  return JSON.stringify(out);
}

export function parseDnsDoc(doc: string): { form: DnsDoc; polRows: KVRow[]; hostRows: KVRow[] } {
  const parsed = parseJson<Partial<DnsDoc>>(doc, {});
  const form: DnsDoc = { ...parsed, enable: parsed.enable ?? false };
  return { form, polRows: kvRows(form.policy), hostRows: kvRows(form.hosts) };
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
  const [polRows, setPolRows] = useState<KVRow[]>(initial.polRows);
  const [hostRows, setHostRows] = useState<KVRow[]>(initial.hostRows);
  const errors = fieldErrors(save.error);
  const submit = (e: FormEvent) => {
    e.preventDefault();
    save.mutate({ node_dns: dnsToJson(form, polRows, hostRows) });
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
        <DNSEditor form={form} onForm={set} polRows={polRows} onPolRows={setPolRows} hostRows={hostRows} onHostRows={setHostRows} />
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
  const [form, setForm] = useState<SubDnsDoc>(() => ({ fake_ip: true, ...(parseJson<Partial<SubDnsDoc>>(doc, {}) as SubDnsDoc) }));
  const [polRows, setPolRows] = useState<{ k: string; v: string }[]>(() =>
    Object.entries(form.policy ?? {}).map(([k, v]) => ({ k, v: v.join(", ") })),
  );
  const errors = fieldErrors(save.error);
  const submit = (e: FormEvent) => {
    e.preventDefault();
    const policy: Record<string, string[]> = {};
    for (const r of polRows) {
      if (!r.k.trim()) continue;
      policy[r.k.trim()] = r.v.split(/[,\s]+/).filter(Boolean);
    }
    const out: SubDnsDoc = { fake_ip: form.fake_ip ?? true };
    const ns = unlines(lines(form.nameservers));
    if (ns.length) out.nameservers = ns;
    const proxy = unlines(lines(form.proxy_servers));
    if (proxy.length) out.proxy_servers = proxy;
    if (Object.keys(policy).length) out.policy = policy;
    if (form.ipv6) out.ipv6 = true;
    save.mutate({ sub_dns: JSON.stringify(out) });
  };
  const set = (k: keyof SubDnsDoc) => (v: SubDnsDoc[keyof SubDnsDoc]) => setForm((f) => ({ ...f, [k]: v }));
  return (
    <section className="card glass max-w-4xl">
      <form onSubmit={submit} noValidate>
        <div className="card-head">
          <div>
            <h2 className="card-title">{t("network.subdns")}</h2>
          </div>
        </div>
        <Field label={t("network.subNameservers")} hint={t("network.subNameserversHint")}>
          <textarea className="input" style={{ minHeight: 56 }} value={lines(form.nameservers)} onChange={(e) => set("nameservers")(unlines(e.target.value))} spellCheck={false} />
        </Field>
        <Field label={t("network.subProxy")}>
          <textarea className="input" style={{ minHeight: 56 }} value={lines(form.proxy_servers)} onChange={(e) => set("proxy_servers")(unlines(e.target.value))} spellCheck={false} />
        </Field>
        <Field label={t("network.subPolicy")}>
          {polRows.map((r, i) => (
            <div key={i} className="mb-2 flex gap-2">
              <input
                className="input max-w-[220px]"
                value={r.k}
                onChange={(e) => setPolRows((rows) => rows.map((x, j) => (j === i ? { ...x, k: e.target.value } : x)))}
                placeholder="geosite:category-ru"
              />
              <input
                className="input"
                value={r.v}
                onChange={(e) => setPolRows((rows) => rows.map((x, j) => (j === i ? { ...x, v: e.target.value } : x)))}
                placeholder="77.88.8.8"
              />
              <Button variant="ghost" onClick={() => setPolRows((rows) => rows.filter((_, j) => j !== i))}>
                {t("network.delete")}
              </Button>
            </div>
          ))}
          <Button variant="glass" onClick={() => setPolRows((rows) => [...rows, { k: "", v: "" }])}>
            {t("network.add")}
          </Button>
        </Field>
        <div className="mb-4 flex flex-wrap gap-4">
          <label className="flex items-center gap-2 text-[13px]">
            <input type="checkbox" className="check" checked={form.fake_ip ?? true} onChange={(e) => set("fake_ip")(e.target.checked)} /> {t("network.subFake")}
          </label>
          <label className="flex items-center gap-2 text-[13px]">
            <input type="checkbox" className="check" checked={!!form.ipv6} onChange={(e) => set("ipv6")(e.target.checked)} /> {t("network.subIpv6")}
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
