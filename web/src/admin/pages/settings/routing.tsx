import { useQuery } from "@tanstack/react-query";
import { api, unwrap } from "../../../api/client";
import { QueryBoundary } from "../../../components/query";
import { Skeleton } from "../../../components/ui";
import { AdBlockCard, GlobalDNSCard, GlobalOutboundsCard, GlobalRoutesCard, SubDNSCard } from "../network";

type RoutingPart = "dns" | "routes" | "outbounds" | "adblock";

function useNetwork() {
  return useQuery({ queryKey: ["network"], queryFn: ({ signal }) => unwrap(api.GET("/api/v1/network", { signal })) });
}

/**
 * Routing: what every node does with a name and an address — the global DNS, the routes,
 * the outbounds and the blocklists. The node's own copies live on its card; the parts are
 * in the URL, so a link opens one.
 */
export function RoutingSection({ part }: { part: RoutingPart }) {
  const net = useNetwork();
  return (
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
  );
}
