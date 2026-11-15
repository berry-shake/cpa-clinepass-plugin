import { describe, expect, it } from "vitest";
import type { CredentialView } from "@core/api/types";
import type { QuotaMap } from "@core/lib/credentials";
import { summarizeKeys, windowLeft, windowOf } from "./lib";
import type { QuotaView } from "./types";

const cred = (auth_index: string, extra: Partial<CredentialView> = {}) => ({ auth_index, ...extra }) as CredentialView;

const usage = (account: string, five: number, weekly?: number): QuotaView => ({
  account,
  fetched_at: "2026-01-02T03:04:05Z",
  windows: [
    { type: "five_hour", percent_used: five },
    weekly === undefined ? { type: "weekly" } : { type: "weekly", percent_used: weekly },
    { type: "monthly", percent_used: 50 },
  ],
});

describe("windows", () => {
  it("reads the share left and leaves windows without a figure blank", () => {
    const d = usage("a", 25);
    expect(windowLeft(windowOf(d, "five_hour"))).toBe(0.75);
    expect(windowLeft(windowOf(d, "weekly"))).toBeNull();
    expect(windowLeft(windowOf(d, "daily"))).toBeNull();
    expect(windowLeft({ type: "x", percent_used: 140 })).toBe(0);
  });
});

describe("summarizeKeys", () => {
  it("counts an account once and averages its windows", () => {
    const quota = {
      k1: { state: "ok", data: usage("acct-1", 20, 100) },
      k2: { state: "ok", data: usage("acct-1", 20, 100) },
      k3: { state: "ok", data: usage("acct-2", 60, 50) },
      k4: { state: "error", error: "boom" },
    } as unknown as QuotaMap;
    const sum = summarizeKeys([cred("k1"), cred("k2"), cred("k3"), cred("k4"), cred("k5"), cred("k6", { disabled: true })], quota);
    expect(sum.accounts).toBe(2);
    expect(sum.exhausted).toBe(1);
    expect(sum.five_hour).toBeCloseTo(0.6);
    expect(sum.weekly).toBeCloseTo(0.25);
    expect(sum.monthly).toBeCloseTo(0.5);
    expect(sum.loaded).toBe(3);
    expect(sum.failed).toBe(1);
    expect(sum.pending).toBe(1);
  });
});
