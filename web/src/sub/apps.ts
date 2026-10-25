export type Platform = "ios" | "android" | "windows" | "macos" | "linux";

type App = {
  name: string;
  note: "easiest" | "free" | "stable" | "openSource" | "modern" | "bestWindows" | "tun" | "oneButton" | "quic";
  /** Bundled icon under sub/apps (web/public), shown instead of the first letter. */
  icon: string;
  link: (url: string, brand: string) => string;
};
export const enc = encodeURIComponent;
const clash = (url: string, brand: string) => `clash://install-config?url=${enc(url)}&name=${enc(brand)}`;
const slothClash = (url: string, brand: string) => `slothclash://install-config?url=${enc(url)}&name=${enc(brand)}`;

export const APPS: Record<Platform, App[]> = {
  ios: [
    { name: "Happ", note: "easiest", icon: "sub/apps/happ.png", link: (u) => `happ://add/${u}` },
    { name: "INCY", note: "modern", icon: "sub/apps/incy.jpg", link: (u) => `incy://add/${u}` },
    { name: "v2RayTun", note: "stable", icon: "sub/apps/v2raytun.webp", link: (u) => `v2raytun://import/${u}` },
    { name: "Hysteria2", note: "quic", icon: "sub/apps/hiddify.png", link: (u) => `hiddify://import/${u}` },
    { name: "TUIC", note: "quic", icon: "sub/apps/hiddify.png", link: (u) => `hiddify://import/${u}` },
  ],
  android: [
    { name: "Happ", note: "easiest", icon: "sub/apps/happ.png", link: (u) => `happ://add/${u}` },
    { name: "INCY", note: "modern", icon: "sub/apps/incy.jpg", link: (u) => `incy://add/${u}` },
    { name: "v2RayTun", note: "stable", icon: "sub/apps/v2raytun.webp", link: (u) => `v2raytun://import/${u}` },
    { name: "Hiddify", note: "openSource", icon: "sub/apps/hiddify.png", link: (u, b) => `hiddify://import/${u}#${enc(b)}` },
  ],
  windows: [
    { name: "Happ", note: "easiest", icon: "sub/apps/happ.png", link: (u) => `happ://add/${u}` },
    { name: "Hiddify", note: "openSource", icon: "sub/apps/hiddify.png", link: (u, b) => `hiddify://import/${u}#${enc(b)}` },
    { name: "Koala Clash", note: "bestWindows", icon: "sub/apps/koala.png", link: (u, b) => `koala-clash://install-config?url=${enc(u)}&name=${enc(b)}` },
  ],
  macos: [
    { name: "Clash Verge Rev", note: "tun", icon: "", link: clash },
    { name: "SlothClash", note: "tun", icon: "", link: slothClash },
    { name: "Happ", note: "oneButton", icon: "sub/apps/happ.png", link: (u) => `happ://add/${u}` },
    { name: "Hiddify", note: "openSource", icon: "sub/apps/hiddify.png", link: (u, b) => `hiddify://import/${u}#${enc(b)}` },
  ],
  linux: [
    { name: "SlothClash", note: "tun", icon: "", link: slothClash },
    { name: "Clash Verge Rev", note: "tun", icon: "", link: clash },
    { name: "Hiddify", note: "openSource", icon: "sub/apps/hiddify.png", link: (u, b) => `hiddify://import/${u}#${enc(b)}` },
  ],
};

export function detect(): Platform {
  const ua = navigator.userAgent;
  if (/iPhone|iPad|iPod/.test(ua)) return "ios";
  if (/Android/.test(ua)) return "android";
  if (/Mac OS X/.test(ua)) return "macos";
  if (/Windows/.test(ua)) return "windows";
  if (/Linux|X11|CrOS/.test(ua)) return "linux"; // after Android: its browsers say Linux too
  return "android";
}
