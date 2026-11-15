// The ClinePass shapes of the management API: the catalog model, and the
// plan usage of the Cline account behind a key.

/** A ClinePass model from Cline's recommended-models list. */
export interface ModelView {
  /** The ID CPA registers, without the cline-pass/ namespace. */
  id: string;
  /** The ID requests carry upstream, e.g. "cline-pass/kimi-k3". */
  upstream_id: string;
  name?: string;
  description?: string;
  tags?: string[];
}

/** One usage window of the plan. */
export interface UsageWindow {
  /** Cline's name: five_hour, weekly or monthly. */
  type: string;
  /** How much of the window is used, 0 to 100; absent when Cline reports no figure. */
  percent_used?: number;
  resets_at?: string;
}

/** The account's plan. */
export interface PlanView {
  name?: string;
  /** End of the current billing period. */
  period_end?: string;
}

/** The plan usage of the account behind a key; account-wide. */
export interface QuotaView {
  /** Same value for keys of the same Cline account. */
  account?: string;
  plan?: PlanView;
  plan_error?: string;
  windows?: UsageWindow[];
  error?: string;
  fetched_at: string;
}

export interface CredentialExtra {}

export interface CatalogExtra {}

export interface CredentialSaveExtra {}

export interface EditorExtra {}
