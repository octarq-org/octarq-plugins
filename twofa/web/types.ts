export interface AccountSummary {
  id: number;
  created_at: string;
  updated_at: string;
  org_id: number;
  name: string;
  issuer: string;
  account: string;
  algorithm: string;
  digits: number;
  period: number;
  tags: string;
  notes: string;
  pinned: boolean;
  current_code?: string;
  seconds_remaining: number;
}

export interface AccountDetail extends AccountSummary {
  secret?: string;
}

export interface AuditLog {
  id: number;
  created_at: string;
  org_id: number;
  account_id: number;
  account_name: string;
  action: string;
  actor: string;
  ip: string;
}

export interface CreateAccountInput {
  name: string;
  issuer?: string;
  account?: string;
  secret?: string;
  algorithm?: string;
  digits?: number;
  period?: number;
  tags?: string;
  notes?: string;
  pinned?: boolean;
  otpauth_url?: string;
}

export interface UpdateAccountInput {
  name?: string;
  issuer?: string;
  account?: string;
  secret?: string;
  algorithm?: string;
  digits?: number;
  period?: number;
  tags?: string;
  notes?: string;
  pinned?: boolean;
}
