import clsx from "clsx";

/**
 * The backdrop behind the glass, "night": deep blue-violet nebula on the left and
 * indigo-cyan on the right. The two drift apart slowly; paper grain on top.
 * `calm` keeps it still (the subscription page, see .atmo.calm in app.css).
 */
export function Atmosphere({ calm }: { calm?: boolean }) {
  return (
    <div className={clsx("atmo", calm && "calm")} aria-hidden>
      <span className="glow glow-warm" />
      <span className="glow glow-cool" />
      <span className="grain" />
    </div>
  );
}

export function Logo({ size = 32 }: { size?: number }) {
  return <img src="logo.png" alt="Cozy" width={size} height={size} style={{ borderRadius: Math.max(6, size / 4) }} />;
}
