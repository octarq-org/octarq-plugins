// DNS feature API surface — domain lifecycle, DNS record CRUD, and DNS verification.

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
            .filter(Boolean);
          if (detailMsgs.length > 0) {
            msg = `${parsed.message}: ${detailMsgs.join(", ")}`;
          }
        }
      } else if (parsed?.error) {
        msg = parsed.error;
      } else if (parsed?.detail) {
        msg = parsed.detail;
      }
    } catch {
      /* not JSON */
    }
    throw new ApiError(res.status, msg, parsed, res.headers.get("x-request-id") ?? undefined);
  }
  if (res.status === 204) return undefined as T;
  const ct = res.headers.get("content-type") || "";
  if (!ct.includes("application/json")) return undefined as T;
  return res.json();
}

export interface HostEntry {
  host: string;
  enabled: boolean;
}

export interface Domain {
  id: number;
  name: string;
  providerAccountId: number;
  zoneId: string;
  note: string;
  forMail: boolean;
  forLink: boolean;
  linkHosts: HostEntry[] | null;
  mailHosts: HostEntry[] | null;
  createdAt: string;
}

export interface ProviderAccount {
  id: number;
  name: string;
  type: string;
  config: Record<string, unknown>;
  hasCredentials: boolean;
  createdAt: string;
  updatedAt: string;
}

export interface ProviderAccountInput {
  name: string;
  type: string;
  config?: Record<string, unknown>;
}

export function effectiveLinkHosts(d: Domain): string[] {
  return (d.linkHosts ?? []).filter((h) => h.enabled).map((h) => h.host);
}

export function effectiveMailHosts(d: Domain): string[] {
  return (d.mailHosts ?? []).filter((h) => h.enabled).map((h) => h.host);
}

export interface DNSRecord {
  id: string;
  type: string;
  name: string;
  content: string;
  ttl: number;
  proxied: boolean;
  comment: string;
  priority?: number | null;
}

export interface DNSRecordStatus {
  set: boolean;
  healthy: boolean;
  value?: string;
}

export interface DKIMStatus extends DNSRecordStatus {
  selector?: string;
}

export interface HostDNSStatus {
  host: string;
  spf: DNSRecordStatus;
  dmarc: DNSRecordStatus;
  dkim: DKIMStatus;
}

export interface LinkHostStatus {
  host: string;
  set: boolean;
  healthy: boolean;
  cname?: string;
  target: string;
}

export interface DNSVerifyResult {
  spf: DNSRecordStatus;
  dmarc: DNSRecordStatus;
  dkim: DKIMStatus;
  hosts: HostDNSStatus[];
  links: LinkHostStatus[];
}

export interface DDNSToken {
  id: number;
  domainId: number;
  recordName: string;
  recordType: string;
  label: string;
  lastIp: string;
  lastSeenAt?: string;
  createdAt: string;
}

export interface CreateDDNSTokenResult {
  id: number;
  domainId: number;
  recordName: string;
  recordType: string;
  label: string;
  secret: string;
  updateUrl: string;
  createdAt: string;
}

export type BlueprintStatus = "ok" | "missing" | "mismatch";

export interface EmailBlueprintRecord {
  type: string;
  name: string;
  content: string;
  ttl: number;
  priority?: number | null;
  status: BlueprintStatus;
}

export interface ApplyBlueprintResult {
  ok: boolean;
  applied: number;
  skipped: number;
}

export const dnsApi = {
  domains: (params?: { q?: string; limit?: number; offset?: number }) => {
    const sp = new URLSearchParams();
    if (params?.q) sp.set("q", params.q);
    if (params?.limit) sp.set("limit", params.limit.toString());
    if (params?.offset) sp.set("offset", params.offset.toString());
    const qs = sp.toString();
    return req<Domain[]>("GET", `/api/domains${qs ? `?${qs}` : ""}`);
  },
  domain: (id: number) => req<Domain>("GET", `/api/domains/${id}`),
  createDomain: (d: any) => req<Domain>("POST", "/api/domains", d),
  updateDomain: (id: number, d: any) => req<Domain>("PUT", `/api/domains/${id}`, d),
  deleteDomain: (id: number) => req("DELETE", `/api/domains/${id}`),
  verifyDNS: (id: number) => req<DNSVerifyResult>("GET", `/api/domains/${id}/verify-dns`),
  records: (id: number) => req<DNSRecord[]>("GET", `/api/domains/${id}/records`),
  createRecord: (id: number, r: Partial<DNSRecord>) =>
    req<DNSRecord>("POST", `/api/domains/${id}/records`, r),
  updateRecord: (id: number, rid: string, r: Partial<DNSRecord>) =>
    req<DNSRecord>("PUT", `/api/domains/${id}/records/${rid}`, r),
  deleteRecord: (id: number, rid: string) => req("DELETE", `/api/domains/${id}/records/${rid}`),
  ddnsTokens: () => req<DDNSToken[]>("GET", "/api/dns/ddns"),
  createDDNSToken: (data: { domainId: number; recordName: string; recordType: string; label: string }) =>
    req<CreateDDNSTokenResult>("POST", "/api/dns/ddns", data),
  deleteDDNSToken: (id: number) => req("DELETE", `/api/dns/ddns/${id}`),
  emailBlueprint: (id: number) => req<EmailBlueprintRecord[]>("GET", `/api/domains/${id}/email-blueprint`),
  applyEmailBlueprint: (id: number) => req<ApplyBlueprintResult>("POST", `/api/domains/${id}/apply-email-blueprint`),
  syncDomains: (providerAccountId: number) =>
    req<{ ok: boolean; total: number; created: number; updated: number }>(
      "POST",
      "/api/domains/sync",
      { providerAccountId },
    ),
  providerAccounts: () => req<ProviderAccount[]>("GET", "/api/provider-accounts"),
  createProviderAccount: (data: any) => req<ProviderAccount>("POST", "/api/provider-accounts", data),
  updateProviderAccount: (id: number, data: any) => req<ProviderAccount>("PUT", `/api/provider-accounts/${id}`, data),
  deleteProviderAccount: (id: number) => req("DELETE", `/api/provider-accounts/${id}`),
  dnsProviders: () => req<string[]>("GET", "/api/dns-providers"),
};

export { dnsApi as api };
