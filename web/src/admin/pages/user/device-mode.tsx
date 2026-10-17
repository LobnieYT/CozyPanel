import { t } from "../../../i18n";

/** How the device limit counts: bound places, or addresses online at once. */
export type DeviceMode = "bound" | "online";

export function modeOf(x: { device_mode?: string | null }): DeviceMode {
  return x.device_mode === "online" ? "online" : "bound";
}

export function DeviceModePicker({ value, onChange }: { value: DeviceMode; onChange: (m: DeviceMode) => void }) {
  return (
    <div className="seg" role="group" aria-label={t("deviceMode.label")}>
      {(["bound", "online"] as const).map((m) => (
        <button key={m} type="button" aria-pressed={value === m} title={m === "bound" ? t("deviceMode.boundHint") : t("deviceMode.onlineHint")} onClick={() => onChange(m)}>
          {m === "bound" ? t("deviceMode.bound") : t("deviceMode.online")}
        </button>
      ))}
    </div>
  );
}
