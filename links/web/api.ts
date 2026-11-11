// Links feature API surface — the slice of the JSON API owned by the links
// plugin. Fully self-contained without relative host leaks.
import type { StatKV } from "@octarq/plugin-sdk";

export class ApiError extends Error {
  status: number;
  body?: any;
  requestId?: string;
  constructor(status: number, message: string, body?: any, requestId?: string) {
    super(message);
    this.status = status;
    this.body = body;
    this.requestId = requestId;
  }
}

export function getAppLang(): string {
  try {
    const saved = localStorage.getItem("lang");
    if (saved) return saved;
  } catch {
    /* ignore */
  }
  return typeof navigator !== "undefined" ? navigator.language || "en" : "en";
}

interface ErrorEnvelope {
  message?: string;
  error?: string;
  detail?: string;
  details?: Array<string | { location?: string; message?: string }>;
}

export async function req<T>(method: string, path: string, body?: unknown, lang?: string): Promise<T> {
  const currentLang = lang || getAppLang();
  const headers: Record<string, string> = {
    "Accept-Language": currentLang,
  };
  if (body) {
    headers["Content-Type"] = "application/json";
  }
  const res = await fetch(path, {
    method,
    headers,
    body: body ? JSON.stringify(body) : undefined,
  });
  if (!res.ok) {
    let msg = res.statusText;
    let parsed: ErrorEnvelope | null = null;
    try {
      parsed = (await res.json()) as ErrorEnvelope;
      if (parsed?.message) {
        msg = parsed.message;
        if (parsed.details && Array.isArray(parsed.details) && parsed.details.length > 0) {
          const detailMsgs = parsed.details
            .map((d) => {
              if (typeof d === "string") return d;
              if (d?.message) {
                const loc = d.location ? `${d.location.replace(/^body\./, "")}: ` : "";
                return `${loc}${d.message}`;
              }
              return "";
            })
            .filter(Boolean)
            .join("; ");
          if (detailMsgs) msg = `${msg} (${detailMsgs})`;
        }
      } else if (parsed?.error) {
        msg = parsed.error;
      } else if (parsed?.detail) {
        msg = parsed.detail;
      }
    } catch {
      // Non-JSON error body: fall through to statusText.
    }
    const requestId = res.headers.get("x-request-id") || undefined;
    throw new ApiError(res.status, msg || `HTTP ${res.status}`, parsed, requestId);
  }
  if (res.status === 204) return undefined as unknown as T;
  const contentType = res.headers.get("content-type");
  if (contentType && contentType.includes("application/json")) {
    return (await res.json()) as T;
  }
  return (await res.text()) as unknown as T;
}

export type { StatKV };

export interface HostEntry {
  host: string;
  enabled: boolean;
}

export interface Domain {
  id: number;
  name: string;
  providerAccountId?: number;
  zoneId?: string;
  note?: string;
  forMail?: boolean;
  forLink?: boolean;
  linkHosts?: HostEntry[] | null;
  mailHosts?: HostEntry[] | null;
  createdAt?: string;
  [key: string]: any;
}

export function effectiveLinkHosts(d: Domain | { hosts?: string[]; linkHosts?: HostEntry[]; name?: string }): string[] {
  if ("linkHosts" in d && Array.isArray(d.linkHosts)) {
    return d.linkHosts.filter((h) => h.enabled).map((h) => h.host);
  }
  if ("hosts" in d && Array.isArray(d.hosts)) {
    return d.hosts;
  }
  return [];
}

export interface Overview extends Record<string, unknown> {
  tokens?: number;
  includeBot?: boolean;
  links?: number;
  activeLinks?: number;
  domains?: number;
  linkDomains?: number;
  mailDomains?: number;
  mailboxes?: number;
  emails?: number;
  unread?: number;
  totalClicks?: number;
  clicks7d?: number;
  clicks30d?: number;
  botClicks7d?: number;
  botClicks30d?: number;
  series?: StatKV[] | null;
  topLinks?: { id: number; slug: string; host: string; title: string; tags: string; clicks: number }[] | null;
  devices?: StatKV[] | null;
  countries?: StatKV[] | null;
  cities?: StatKV[] | null;
  recentEmails?: { id: number; from: string; subject: string; read: boolean; receivedAt: string }[] | null;
}

export interface Link {
  id: number;
  host: string;
  slug: string;
  target: string;
  note: string;
  title: string;
  tags: string;
  expiresAt: string | null;
  expiredUrl: string;
  clickLimit: number;
  archived: boolean;
  enabled: boolean;
  clicks: number;
  password?: string;
  hasPassword: boolean;
  routingRules?: any[];
  createdAt: string;
}

export interface LinkStats {
  total: number;
  windowed: number;
  days: number;
  metric?: "pv" | "uv";
  series: StatKV[];
  referers: StatKV[] | null;
  channels: StatKV[] | null;
  countries: StatKV[] | null;
  regions: StatKV[] | null;
  devices: StatKV[] | null;
  browsers: StatKV[] | null;
  utmSources: StatKV[] | null;
  utmMediums: StatKV[] | null;
  utmCampaigns: StatKV[] | null;
  variants: StatKV[] | null;
}

export interface LinksCapabilities {
  geoReady: boolean;
  mode: "configured" | "unavailable";
}

export const linksApi = {
  links: (params: { q?: string; tag?: string; host?: string; archived?: boolean; limit?: number; offset?: number } = {}) => {
    const sp = new URLSearchParams();
    if (params.q) sp.set("q", params.q);
    if (params.tag) sp.set("tag", params.tag);
    if (params.host) sp.set("host", params.host);
    if (params.archived) sp.set("archived", "1");
    if (params.limit) sp.set("limit", params.limit.toString());
    if (params.offset) sp.set("offset", params.offset.toString());
    const qs = sp.toString();
    return req<Link[]>("GET", `/api/links${qs ? `?${qs}` : ""}`);
  },
  createLink: (l: Partial<Link> & { password?: string }) => req<Link>("POST", "/api/links", l),
  updateLink: (id: number, l: Partial<Link> & { password?: string }) =>
    req<Link>("PUT", `/api/links/${id}`, l),
  deleteLink: (id: number) => req("DELETE", `/api/links/${id}`),
  linkStats: (id: number, days = 30, metric: "pv" | "uv" = "uv") =>
    req<LinkStats>("GET", `/api/links/${id}/stats?days=${days}&metric=${metric}`),
  linkMetadata: (url: string) =>
    req<{ title: string; description: string; favicon: string }>(
      "GET",
      `/api/links/metadata?url=${encodeURIComponent(url)}`,
    ),
  capabilities: () => req<LinksCapabilities>("GET", "/api/links/capabilities"),
  domains: (q?: { q?: string; limit?: number; offset?: number }) => {
    const sp = new URLSearchParams();
    if (q?.q) sp.set("q", q.q);
    if (q?.limit) sp.set("limit", String(q.limit));
    if (q?.offset) sp.set("offset", String(q.offset));
    const qs = sp.toString();
    return req<Domain[]>("GET", `/api/domains${qs ? `?${qs}` : ""}`);
  },
  overview: (includeBot = false) => req<Overview>("GET", `/api/overview?include_bot=${includeBot}`),
  aiAssistStatus: () => req<{ configured: boolean; provider: string }>("GET", "/api/ai/assist/status"),
  aiSuggestSlug: (target: string, title?: string) =>
    req<{ slugs: string[] }>("POST", "/api/ai/assist/suggest-slug", { target, title }),
};

export { linksApi as api };
