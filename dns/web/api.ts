
import { req } from "@octarq/plugin-sdk";


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
  dnsProviders: () => req<string[]>("GET", "/api/dns/providers"),
};

export { dnsApi as api };
