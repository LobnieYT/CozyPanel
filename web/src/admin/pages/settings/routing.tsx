import { useQuery } from "@tanstack/react-query";
import { type FormEvent, useState } from "react";
import { api, unwrap, type Schemas } from "../../../api/client";
import { QueryBoundary } from "../../../components/query";
import { Button, Skeleton } from "../../../components/ui";
import { useGrants } from "../../../api/hooks";
import { t } from "../../../i18n";
import { AdBlockCard, GlobalDNSCard, GlobalOutboundsCard, GlobalRoutesCard, SubDNSCard } from "../network";
import { useSaveSettings } from "./shared";

type RoutingPart = "dns" | "routes" | "outbounds" | "adblock";

function useNetwork() {
  return useQuery({ queryKey: ["network"], queryFn: ({ signal }) => unwrap(api.GET("/api/v1/network", { signal })) });
}

/**
 * Routing: what every node does with a name and an address — the global DNS, the routes,
 * the outbounds and the blocklists. The node's own copies live on its card; the parts are
 * in the URL, so a link opens one.
 */
export function RoutingSection({ part, s }: { part: RoutingPart; s: Schemas["SettingsView"] }) {
  const net = useNetwork();
  return (
    <>
    <RoutingModeCard s={s} />
    <QueryBoundary query={net} pending={<Skeleton style={{ height: 320, borderRadius: 20 }} />}>
        {(n) =>
          part === "dns" ? (
            <GlobalDNSCard key="dns" doc={n.node_dns} />
          ) : part === "routes" ? (
            <GlobalRoutesCard key="routes" doc={n.node_routes} />
          ) : part === "outbounds" ? (
            <GlobalOutboundsCard key="outbounds" doc={n.node_outbounds} />
          ) : (
            <div className="flex flex-col gap-4">
              <AdBlockCard nodeDoc={n.node_adblock} subDoc={n.sub_adblock} />
              <SubDNSCard doc={n.sub_dns} />
            </div>
          )
        }
    </QueryBoundary>
    </>
  );
}

// Where the apps route: Russian sites straight out, everything else through the VPN, or
// everything through it.
const ROUTING_MODES = [
  { id: "ru_direct", title: "settings.routingRuDirect", sub: "settings.routingRuDirectSub" },
  { id: "all", title: "settings.routingAll", sub: "settings.routingAllSub" },
] as const;

/** The choice of where the apps send traffic: it belongs to routing, not to the profile. */
function RoutingModeCard({ s }: { s: Schemas["SettingsView"] }) {
  const save = useSaveSettings();
  const { can } = useGrants();
  const writable = can("settings", true);
  const saved = s.sub_routing;
  const [mode, setMode] = useState<Schemas["SettingsView"]["sub_routing"]>(saved);
  const changed = mode !== saved;
  const submit = (e: FormEvent) => {
    e.preventDefault();
    save.mutate({ sub_routing: mode });
  };
  return (
    <section className="card glass">
      <form onSubmit={submit} noValidate>
        <div className="card-head">
          <div>
            <h2 className="card-title">{t("settings.routing")}</h2>
            <div className="card-sub">{t("settings.routingHint")}</div>
          </div>
        </div>
        <div className="grid gap-2 sm:grid-cols-2" role="radiogroup" aria-label={t("settings.routing")}>
          {ROUTING_MODES.map((m) => (
            <button key={m.id} type="button" role="radio" aria-checked={mode === m.id} className="opt" disabled={!writable} onClick={() => setMode(m.id)}>
              <span className="font-semibold">{t(m.title)}</span>
              <span className="text-xs text-[var(--ink-500)]">{t(m.sub)}</span>
            </button>
          ))}
        </div>
        {writable ? (
          <Button type="submit" variant="primary" loading={save.isPending} disabled={!changed} className="mt-4">
            {t("common.save")}
          </Button>
        ) : null}
      </form>
    </section>
  );
}
