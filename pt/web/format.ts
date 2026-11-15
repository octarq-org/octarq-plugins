// Display helpers. Traffic is 1024-based, which is what every tracker reports and
// what users expect from "GB" on a PT site. A missing value renders as the
// caller's unknown label, never as zero.

const UNITS = ["B", "KiB", "MiB", "GiB", "TiB", "PiB"] as const;

export function formatBytes(value: number | undefined, unknown: string): string {
  if (value === undefined || value === null || !Number.isFinite(value)) return unknown;
  if (value < 0) return unknown;
  let size = value;
  let unit = 0;
  while (size >= 1024 && unit < UNITS.length - 1) {
    size /= 1024;
    unit += 1;
  }
  const digits = unit === 0 ? 0 : size >= 100 ? 1 : 2;
  return `${size.toFixed(digits)} ${UNITS[unit]}`;
}

export function formatRatio(ratio: number | undefined, infinite: boolean, unknown: string): string {
  if (infinite) return "∞";
  if (ratio === undefined || ratio === null || !Number.isFinite(ratio)) return unknown;
  return ratio.toFixed(3);
}

export function formatNumber(value: number | undefined, unknown: string, digits = 2): string {
  if (value === undefined || value === null || !Number.isFinite(value)) return unknown;
  return value.toLocaleString(undefined, { maximumFractionDigits: digits });
}

export function formatDateTime(value: string | undefined, unknown: string, locale?: string): string {
  if (!value) return unknown;
  const parsed = new Date(value);
  if (Number.isNaN(parsed.getTime())) return unknown;
  return parsed.toLocaleString(locale, {
    year: "numeric",
    month: "2-digit",
    day: "2-digit",
    hour: "2-digit",
    minute: "2-digit",
  });
}

export type BadgeToneName = "default" | "info" | "success" | "warning" | "danger";

// outcomeTone maps a sign-in verdict to a badge tone. "Not confirmed" stays
// amber on purpose: it is not a failure, but it is not a success either.
export function outcomeTone(outcome: string | undefined): BadgeToneName {
  switch (outcome) {
    case "success":
      return "success";
    case "already_signed":
      return "info";
    case "login_ok_unconfirmed":
      return "warning";
    case "needs_manual_action":
      return "warning";
    case "auth_failed":
    case "parse_error":
      return "danger";
    case "rate_limited":
    case "network_error":
      return "warning";
    case "unsupported":
      return "default";
    default:
      return "default";
  }
}

// outcomeKey maps a sign-in verdict to its translation key.
export function outcomeKey(outcome: string | undefined): string {
  switch (outcome) {
    case "success":
      return "outcomeSuccess";
    case "already_signed":
      return "outcomeAlready";
    case "login_ok_unconfirmed":
      return "outcomeUnconfirmed";
    case "needs_manual_action":
      return "outcomeManual";
    case "auth_failed":
      return "outcomeAuthFailed";
    case "rate_limited":
      return "outcomeRateLimited";
    case "network_error":
      return "outcomeNetwork";
    case "parse_error":
      return "outcomeParse";
    case "unsupported":
      return "outcomeUnsupported";
    default:
      return "outcomePending";
  }
}

// minutesToClock renders a minutes-from-midnight window bound as HH:MM.
export function minutesToClock(minutes: number): string {
  const bounded = ((minutes % 1440) + 1440) % 1440;
  const hh = Math.floor(bounded / 60).toString().padStart(2, "0");
  const mm = (bounded % 60).toString().padStart(2, "0");
  return `${hh}:${mm}`;
}

export function clockToMinutes(clock: string): number | undefined {
  const match = /^(\d{1,2}):(\d{2})$/.exec(clock.trim());
  if (!match) return undefined;
  const hours = Number(match[1]);
  const minutes = Number(match[2]);
  if (hours > 23 || minutes > 59) return undefined;
  return hours * 60 + minutes;
}
