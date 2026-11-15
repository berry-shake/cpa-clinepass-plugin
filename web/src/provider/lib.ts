// ClinePass figures: the plan windows and the summary totals.

import type { CredentialView } from "@core/api/types";
import type { QuotaMap } from "@core/lib/credentials";
import type { QuotaView, UsageWindow } from "./types";

/** The windows a card shows, in order. */
export const WINDOWS = ["five_hour", "weekly", "monthly"] as const;

/** The window of a type, if Cline reported it. */
export function windowOf(d: QuotaView | null | undefined, type: string): UsageWindow | undefined {
  return (d && Array.isArray(d.windows) ? d.windows : []).find((w) => w.type === type);
}

/** The share of a window still available, 0..1; null without a figure. */
export function windowLeft(w: UsageWindow | undefined): number | null {
  if (!w || typeof w.percent_used !== "number") return null;
  return Math.max(0, Math.min(1, 1 - w.percent_used / 100));
}

/** Whether a window is used up. */
export function exhausted(w: UsageWindow | undefined): boolean {
  return windowLeft(w) === 0;
}

/** Totals for the summary strip of the credentials tab. */
export interface KeySummary {
  /** Distinct Cline accounts among the keys whose figures loaded. */
  accounts: number;
  /** Accounts with a window used up. */
  exhausted: number;
  /** Average share left per window across the accounts, or null. */
  five_hour: number | null;
  weekly: number | null;
  monthly: number | null;
  loaded: number;
  pending: number;
  failed: number;
  loading: boolean;
}

function average(values: (number | null)[]): number | null {
  const known = values.filter((v): v is number => v !== null);
  return known.length ? known.reduce((a, b) => a + b, 0) / known.length : null;
}

// The plan belongs to the account, so keys of the same account (same
// account value) are counted once.
export function summarizeKeys(creds: CredentialView[], quota: QuotaMap): KeySummary {
  const accounts = new Map<string, QuotaView>();
  const sum: KeySummary = {
    accounts: 0, exhausted: 0, five_hour: null, weekly: null, monthly: null,
    loaded: 0, pending: 0, failed: 0, loading: false,
  };
  for (const cred of creds) {
    if (cred.read_error) continue;
    const q = quota[cred.auth_index];
    if (q && q.state === "loading") sum.loading = true;
    const d = q && q.data;
    if (!d) {
      if (q && q.state === "error") sum.failed++;
      else if (!cred.disabled) sum.pending++;
      continue;
    }
    sum.loaded++;
    if (!Array.isArray(d.windows)) continue;
    const account = d.account || "auth:" + cred.auth_index;
    if (!accounts.has(account)) accounts.set(account, d);
  }
  const all = [...accounts.values()];
  sum.accounts = all.length;
  sum.exhausted = all.filter((d) => WINDOWS.some((type) => exhausted(windowOf(d, type)))).length;
  sum.five_hour = average(all.map((d) => windowLeft(windowOf(d, "five_hour"))));
  sum.weekly = average(all.map((d) => windowLeft(windowOf(d, "weekly"))));
  sum.monthly = average(all.map((d) => windowLeft(windowOf(d, "monthly"))));
  return sum;
}
