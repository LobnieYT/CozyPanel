import { useNavigate, useSearch } from "@tanstack/react-router";
import { useQuery } from "@tanstack/react-query";
import { Cpu, Globe, LayoutTemplate, Link2, ListFilter, Route, ShieldCheck } from "lucide-react";
import { meQuery, useSettings } from "../../../api/hooks";
import { LangSwitch } from "../../../components/lang";
import { QueryBoundary } from "../../../components/query";
import { SectionNav } from "../../../components/section-nav";
import { PageHeader, Skeleton } from "../../../components/ui";
import { t, type Key } from "../../../i18n";
import { SETTINGS_PARTS, SETTINGS_TABS, type SettingsSearch } from "../../search";
import { AutoCard, EgressCard, LanguageCard, MonitoringCard, ServerCard, UpdatesCard } from "./general";
import { ClashRulesCard } from "./rules";
import { AccessCard, AdminBotCard, ApiCard, CertificateCard, PasswordCard, SessionsCard, TwoFactorCard } from "./security";
import { DevicesCard, SubscriptionCard, SubPortCard } from "./subscription";
import { RoutingSection } from "./routing";
import { SubPageSettings, type SubPagePart } from "./sub-page";

type Tab = SettingsSearch["tab"];
type RoutingPart = "dns" | "routes" | "outbounds" | "adblock";

const PAGE_PARTS: SubPagePart[] = ["look", "brand", "blocks", "apps", "css"];

const PART_LABELS: Record<string, Key> = {
  look: "settings.page.look",
  brand: "settings.page.brand",
  blocks: "settings.page.blocks",
  apps: "settings.page.apps",
  css: "settings.page.css",
  dns: "settings.routingParts.dns",
  routes: "settings.routingParts.routes",
  outbounds: "settings.routingParts.outbounds",
  adblock: "settings.routingParts.adblock",
};

const ICONS = { general: Globe, subscription: Link2, rules: ListFilter, routing: Route, page: LayoutTemplate, security: ShieldCheck, system: Cpu } as const;



/**
 * Settings in sections listed beside the content (a select on phones); the section and its
 * part are in the URL, so a link opens them. Forms keep one readable width; the editors with
 * a live preview take the whole column.
 */
export function SettingsPage() {
  const settings = useSettings();
  const me = useQuery(meQuery);
  const { tab, part } = useSearch({ from: "/_app/settings" });
  const routingPart: RoutingPart = (["dns", "routes", "outbounds", "adblock"] as const).find((x) => x === part) ?? "dns";
  const pagePart: SubPagePart = PAGE_PARTS.find((x) => x === part) ?? "look";
  const navigate = useNavigate({ from: "/settings" });
  const go = (next: Tab, nextPart?: string) => void navigate({ search: { tab: next, part: nextPart }, replace: true });
  const wide = tab === "routing";
  return (
    <>
      <PageHeader title={t("nav.settings")} sub={t("settings.subtitle")} actions={<LangSwitch />} />
      <SectionNav
        label={t("settings.sections")}
        sections={SETTINGS_TABS.map((id) => ({
          id,
          label: t(`settings.tabs.${id}`),
          icon: ICONS[id],
          parts: id in SETTINGS_PARTS ? SETTINGS_PARTS[id as keyof typeof SETTINGS_PARTS].map((p) => ({ id: p, label: t(PART_LABELS[p]!) })) : undefined,
        }))}
        value={tab}
        part={tab === "routing" ? routingPart : tab === "page" ? pagePart : undefined}
        onChange={go}
        narrow={!wide}
      >
        {tab === "page" ? (
          // The editor keeps its own draft: leaving the section drops it.
          <SubPageSettings section={pagePart} />
        ) : (
          <QueryBoundary query={settings} pending={<Skeleton style={{ height: 320, borderRadius: 20 }} />} wrap={(state) => <section className="card glass">{state}</section>}>
            {(s) =>
              tab === "general" ? (
                <>
                  <ServerCard s={s} />
                  <LanguageCard s={s} />
                  <EgressCard s={s} />
                  <MonitoringCard s={s} />
                </>
              ) : tab === "subscription" ? (
                <>
                  <SubscriptionCard s={s} />
                  <DevicesCard s={s} />
                  <SubPortCard s={s} />
                </>
              ) : tab === "routing" ? (
                <>
                  <RoutingSection part={routingPart} />
                </>
              ) : tab === "rules" ? (
                <ClashRulesCard s={s} />
              ) : tab === "system" ? (
                <>
                  <UpdatesCard />
                  <AutoCard s={s} />
                </>
              ) : (
                <>
                  <AccessCard s={s} />
                  <CertificateCard s={s} />
                  <ApiCard />
                  <AdminBotCard owner={!!me.data?.admin.owner} />
                  <PasswordCard />
                  <TwoFactorCard />
                  <SessionsCard />
                </>
              )
            }
          </QueryBoundary>
        )}
      </SectionNav>
    </>
  );
}
