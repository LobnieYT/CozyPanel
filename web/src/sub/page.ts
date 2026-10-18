// What the admin made of the subscription page (internal/panel/subpage): the server puts it
// into the page's HTML as a JSON block, so the page knows its look before anything loads.
import type { CSSProperties } from "react";
import type { components } from "../api/schema";

export type PageConfig = components["schemas"]["Page"];
export type PageBlock = components["schemas"]["PageBlock"];
export type PageLook = components["schemas"]["PageLook"];

type PagePublic = {
  config: PageConfig;
  brand: string;
  /** The brand's colour when the page takes it; none keeps the palette's. */
  accent?: string;
};

export const PLATFORMS = ["ios", "android", "windows", "macos", "linux"] as const;

/** The page as it was before it could be configured: what the server sends with no save. */
const DEFAULT_PAGE: PageConfig = {
  look: { palette: "mikan", mode: "light", accent: "theme", font: "default", radius: "medium", cards: "glass", background: { kind: "theme", from: "", to: "", angle: 160, dim: 30 } },
  brand: { logo: "letter", emoji: "", subtitle: "" },
  blocks: ["announce", "status", "promo", "shop", "traffic", "devices", "apps", "guide", "instructions", "link", "reissue", "locations", "telegram", "support"].map((type) => ({
    id: type,
    type,
    on: type !== "announce" && type !== "locations",
    title: "",
  })) as PageBlock[],
  apps: { platform: "auto", ios: { order: [], hidden: [] }, android: { order: [], hidden: [] }, windows: { order: [], hidden: [] }, macos: { order: [], hidden: [] }, linux: { order: [], hidden: [] }, qr: true },
  og: { title: "", description: "", image: false },
  css: "",
};

/** The page's data from its HTML; the default page when it has none (the Vite dev server). */
export function readPage(): PagePublic {
  try {
    const raw = document.getElementById("cozy-page")?.textContent;
    if (raw) {
      const p = JSON.parse(raw) as PagePublic;
      if (p && p.config) return { ...p, config: { ...DEFAULT_PAGE, ...p.config } };
    }
  } catch {
    // A broken block is the default page, not an empty one.
  }
  return { config: DEFAULT_PAGE, brand: "" };
}

/** "system" follows the visitor's scheme as it changes. */
export function schemeOf(mode: string, systemDark: boolean): "light" | "dark" {
  return mode === "system" ? (systemDark ? "dark" : "light") : mode === "dark" ? "dark" : "light";
}

/** The backdrop's own CSS when the admin chose one; null keeps the theme's. */
export function backdropStyle(look: PageLook): CSSProperties | null {
  const b = look.background;
  switch (b.kind) {
    case "solid":
      return b.from ? { background: b.from } : null;
    case "gradient":
      return b.from && b.to ? { background: `linear-gradient(${b.angle}deg, ${b.from}, ${b.to})` } : null;
    default:
      return null;
  }
}

/**
 * Puts the page's look on its <html> before the first paint (no flash of the default
 * look): the brand's colour as the accent, the font, the corners and the cards.
 */
export function applyLook(page: PagePublic) {
  const root = document.documentElement;
  const look = page.config.look;
  const dark = window.matchMedia?.("(prefers-color-scheme: dark)");
  const paint = () => {
    const scheme = schemeOf(look.mode, !!dark?.matches);
    root.dataset.look = look.palette;
    root.dataset.scheme = scheme;
    root.dataset.font = look.font;
    root.dataset.radius = look.radius;
    root.dataset.cards = look.cards;
    // The brand's colour takes the panel's own accent, so every button follows it.
    if (look.accent === "brand" && page.accent) {
      root.style.setProperty("--cozy-500", page.accent);
      root.style.setProperty("--cozy-700", page.accent);
    } else {
      root.style.removeProperty("--cozy-500");
      root.style.removeProperty("--cozy-700");
    }
  };
  paint();
  if (look.mode === "system") dark?.addEventListener?.("change", paint);
}

/** The admin's CSS as the server keeps it (subpage.CleanCSS). */
export function cleanCSS(css: string): string {
  let out = css.replaceAll("<", "").replaceAll("\0", "").replaceAll("\\", "");
  for (;;) {
    const next = out.replace(/@import/gi, "").replace(/(url|image-set|image)\(\s*['"]?\s*(https?:|\/\/)[^)]*\)/gi, "none");
    if (next === out) break;
    out = next;
  }
  return out.trim();
}
