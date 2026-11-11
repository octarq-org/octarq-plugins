// Mail feature API surface — the slice of the JSON API owned by the mail plugin.
// Fully self-contained without relative host leaks.

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

export function effectiveMailHosts(d: Domain | { hosts?: string[]; mailHosts?: HostEntry[]; name?: string }): string[] {
  if ("mailHosts" in d && Array.isArray(d.mailHosts)) {
    return d.mailHosts.filter((h) => h.enabled).map((h) => h.host);
  }
  if ("hosts" in d && Array.isArray(d.hosts)) {
    return d.hosts;
  }
  return [];
}

export interface SMTPSender {
  id: number;
  name: string;
  host: string;
  port: number;
  user: string;
  fromEmail: string;
  passSet: boolean;
  createdAt: string;
}

export interface SMTPSenderInput {
  name: string;
  host: string;
  port: number;
  user: string;
  fromEmail: string;
  pass?: string;
}

export interface MailSettingsData {
  reservedMailboxes?: string;
  catchAll?: boolean;
  autoWrapLinks?: boolean;
  inboundTokenSet?: boolean;
  orgSlug?: string;
  [key: string]: unknown;
}

export interface Mailbox {
  id: number;
  address: string;
  note: string;
  enabled: boolean;
  unread: number;
}

export interface Attachment {
  filename: string;
  contentType: string;
  size: number;
  inline?: boolean;
  contentId?: string;
  truncated?: boolean;
}

export interface Email {
  id: number;
  mailboxId: number;
  from: string;
  to: string;
  subject: string;
  text: string;
  html: string;
  folder?: string;
  unsubscribeUrl?: string;
  read: boolean;
  note: string;
  attachments: string; // JSON string of Attachment[]
  authSpf: string;     // pass|fail|softfail|neutral|none|""
  authDkim: string;    // pass|fail|none|""
  authDmarc: string;   // pass|fail|none|""
  receivedAt: string;
  category?: "bill" | "otp" | "marketing" | "important" | "general" | string;
  summary?: string;
  importance?: number;
  otp?: string;
}

export interface MailContact {
  id: number;
  address: string;
  name: string;
  interactionCount: number;
  lastSeenAt: string;
  createdAt?: string;
  updatedAt?: string;
}

export interface MailSuppression {
  id: number;
  address: string;
  reason: string;
  source: string;
  count: number;
  createdAt: string;
  updatedAt: string;
}

export const mailApi = {
  // Mailbox CRUD
  mailboxes: () => req<Mailbox[]>("GET", "/api/mailboxes"),
  createMailbox: (m: Partial<Mailbox>) => req<Mailbox>("POST", "/api/mailboxes", m),
  updateMailbox: (id: number, m: Partial<Mailbox>) => req<Mailbox>("PUT", `/api/mailboxes/${id}`, m),
  deleteMailbox: (id: number) => req("DELETE", `/api/mailboxes/${id}`),

  // Email read & mutation
  emails: (mailboxId?: number, params?: { q?: string; folder?: string; limit?: number; offset?: number }) => {
    const sp = new URLSearchParams();
    if (mailboxId) sp.set("mailbox", mailboxId.toString());
    if (params?.folder) sp.set("folder", params.folder);
    if (params?.q) sp.set("q", params.q);
    if (params?.limit) sp.set("limit", params.limit.toString());
    if (params?.offset) sp.set("offset", params.offset.toString());
    const qs = sp.toString();
    return req<Email[]>("GET", `/api/emails${qs ? "?" + qs : ""}`);
  },
  email: (id: number) => req<Email>("GET", `/api/emails/${id}`),
  updateEmail: (id: number, e: { read?: boolean; note?: string }) =>
    req<Email>("PUT", `/api/emails/${id}`, e),
  updateEmailFolder: (id: number, folder: string) =>
    req<Email>("PUT", `/api/mail/emails/${id}/folder`, { folder }),
  deleteEmail: (id: number) => req("DELETE", `/api/emails/${id}`),
  readAllEmails: (mailbox?: number) =>
    req<{ ok: boolean; updated: number }>(
      "POST",
      `/api/emails/read-all${mailbox ? `?mailbox=${mailbox}` : ""}`,
    ),
  rawEmailUrl: (id: number) => `/api/emails/${id}/raw`,
  sendEmail: (m: {
    from?: string;
    to: string[];
    subject: string;
    text?: string;
    html?: string;
    smtpSenderId?: number;
    smtpSenderSource?: "local" | "integration";
    trackLinks?: boolean;
  }) => req("POST", "/api/emails/send", m),

  // Contacts
  contacts: (params?: { query?: string; q?: string; limit?: number; offset?: number }) => {
    const sp = new URLSearchParams();
    const q = params?.query ?? params?.q;
    if (q) sp.set("q", q);
    if (params?.limit) sp.set("limit", params.limit.toString());
    if (params?.offset) sp.set("offset", params.offset.toString());
    const qs = sp.toString();
    return req<MailContact[]>("GET", `/api/mail/contacts${qs ? "?" + qs : ""}`);
  },

  // Drafts
  saveDraft: (d: { id?: number; mailboxId?: number; to: string; subject: string; text: string; html?: string }) =>
    req<Email>("POST", "/api/mail/drafts", d),
  deleteDraft: (id: number) => req<{ ok: boolean }>("DELETE", `/api/mail/drafts/${id}`),

  // Suppressions
  suppressions: () => req<MailSuppression[]>("GET", "/api/mail/suppressions"),
  createSuppression: (address: string) => req<MailSuppression>("POST", "/api/mail/suppressions", { address }),
  deleteSuppression: (id: number) => req("DELETE", `/api/mail/suppressions/${id}`),

  // Domains (used for mailbox creation host selection)
  domains: (params?: { q?: string; limit?: number; offset?: number }) => {
    const sp = new URLSearchParams();
    if (params?.q) sp.set("q", params.q);
    if (params?.limit) sp.set("limit", params.limit.toString());
    if (params?.offset) sp.set("offset", params.offset.toString());
    const qs = sp.toString();
    return req<Domain[]>("GET", `/api/domains${qs ? "?" + qs : ""}`);
  },

  // SMTP Senders
  smtpSenders: () => req<SMTPSender[]>("GET", "/api/smtp-senders"),
  createSMTPSender: (s: SMTPSenderInput) => req<SMTPSender>("POST", "/api/smtp-senders", s),
  updateSMTPSender: (id: number, s: Partial<SMTPSenderInput>) =>
    req<SMTPSender>("PUT", `/api/smtp-senders/${id}`, s),
  deleteSMTPSender: (id: number) => req<void>("DELETE", `/api/smtp-senders/${id}`),
  testSMTPSender: (id: number) => req<{ ok: boolean }>("POST", `/api/smtp-senders/${id}/test`),

  // Settings & Inbound Token
  settings: () => req<MailSettingsData>("GET", "/api/settings"),
  updateSettings: (s: Partial<MailSettingsData> & { inboundToken?: string }) =>
    req<any>("PUT", "/api/settings", s),
  inboundToken: () => req<{ inboundToken: string }>("GET", "/api/settings/inbound-token"),

  // AI Assist
  aiAssistStatus: () => req<{ configured: boolean; provider: string }>("GET", "/api/ai/assist/status"),
  aiSummarizeEmail: (id: number) =>
    req<{ summary: string; category?: string; importance?: number; otp?: string }>(
      "POST",
      `/api/ai/assist/summarize-email/${id}`,
    ),

  // Overview
  overview: (includeBot = false) => req<any>("GET", `/api/overview?include_bot=${includeBot}`),
};

export { mailApi as api };
