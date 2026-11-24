import { getAppLang, pluginURL, req, type StatKV } from "@octarq/plugin-sdk";


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

export const linksAiStatus = () => req<{ configured: boolean; provider: string }>("GET", "/api/links/ai/status");
export const suggestLinkSlug = (target: string, title?: string) =>
  req<{ slugs: string[] }>("POST", "/api/links/ai/suggest-slug", { target, title });

export interface AbuseReport {
  id: number;
  slug: string;
  target: string;
  reason: "spam" | "phishing" | "malware" | "other" | string;
  description: string;
  reporterIp: string;
  status: "open" | "reviewed" | "dismissed" | string;
  createdAt: string;
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
  exportCsv: async (): Promise<Blob> => {
    const res = await fetch(pluginURL("/api/links/export.csv"), {
      credentials: "same-origin",
      headers: { "Accept-Language": getAppLang() },
    });
    if (!res.ok) {
      throw new Error(`HTTP ${res.status}`);
    }
    return res.blob();
  },
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
  linksAiStatus,
  suggestLinkSlug,
  abuseReports: (status?: string) => {
    const qs = status ? `?status=${encodeURIComponent(status)}` : "";
    return req<AbuseReport[]>("GET", `/api/links/abuse${qs}`);
  },
  updateAbuseReport: (id: number, status: string) =>
    req<AbuseReport>("PUT", `/api/links/abuse/${id}`, { status }),
};

export { linksApi as api };
