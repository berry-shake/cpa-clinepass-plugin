import { Bar, Meter, StatItem, UsagePending } from "@core/components/keys/parts";
import type { KeyCardProps, ProviderComponents } from "@core/contract";
import type { CredentialView } from "@core/api/types";
import type { T } from "@core/i18n";
import { formatShort } from "@core/lib/format";
import type { State } from "@core/state/store";
import { WINDOWS, exhausted, summarizeKeys, windowLeft, windowOf } from "./lib";
import type { ModelView, QuotaView, UsageWindow } from "./types";

// Totals over the Cline accounts whose figures have been loaded; keys of the
// same account count once.
function KeySummary({ t, s, creds }: { t: T; s: State; creds: CredentialView[] }) {
  const sum = summarizeKeys(creds, s.quota);
  const blank = sum.loading ? "…" : "—";
  const pct = (v: number | null) => (v === null ? blank : Math.round(v * 100) + "%");
  const missingNote: string[] = [];
  if (sum.failed) missingNote.push(t("summary.failed", { n: sum.failed }));
  if (sum.pending) missingNote.push(t("summary.pending", { n: sum.pending }));
  return (
    <div className="summary strip" id="key-summary">
      <StatItem
        label={t("summary.accounts")}
        value={sum.accounts || sum.loaded ? String(sum.accounts) : blank}
        note={sum.exhausted ? t("summary.exhausted", { n: sum.exhausted }) : ""}
      />
      <StatItem label={t("usage.left.five_hour")} value={pct(sum.five_hour)} note={sum.accounts > 1 ? t("summary.average") : ""} />
      <StatItem label={t("usage.left.weekly")} value={pct(sum.weekly)} />
      <StatItem label={t("usage.left.monthly")} value={pct(sum.monthly)} note={missingNote.join(" · ")} />
    </div>
  );
}

function pctText(left: number | null): string {
  return left === null ? "—" : Math.round(left * 100) + "%";
}

/** Used share and reset time of a window, for its tooltip. */
function windowTip(t: T, w: UsageWindow | undefined, locale: string): string {
  if (!w) return "";
  return [
    typeof w.percent_used === "number" ? t("usage.used", { v: Math.round(w.percent_used) + "%" }) : t("usage.noFigure"),
    w.resets_at ? t("usage.resets", { time: formatShort(w.resets_at, locale) }) : "",
  ].filter(Boolean).join(" · ");
}

/** When the plan lets requests through again: the latest reset among the used-up windows; null when none is. */
function exhaustedUntil(d: QuotaView): string | null {
  let until: string | null = null;
  let any = false;
  for (const type of WINDOWS) {
    const w = windowOf(d, type);
    if (!exhausted(w)) continue;
    any = true;
    if (w && w.resets_at && (!until || new Date(w.resets_at) > new Date(until))) until = w.resets_at;
  }
  return any ? until || "" : null;
}

// The card's plan windows (or why there are none), the monthly bar and notes.
function KeyUsage({ t, cred, q, locale }: KeyCardProps) {
  if (cred.read_error) return null;
  const d = q && q.data;
  if (!d) return <UsagePending t={t} cred={cred} q={q} />;
  const until = exhaustedUntil(d);
  const monthly = windowOf(d, "monthly");
  const monthlyLeft = windowLeft(monthly);
  const plan = d.plan || {};
  const monthlyLabel = [
    plan.name || t("usage.window.monthly"),
    monthly && monthly.resets_at ? t("usage.resets", { time: formatShort(monthly.resets_at, locale) }) : "",
  ].filter(Boolean).join(" · ");
  return (
    <>
      <div className="meters">
        {WINDOWS.map((type) => {
          const w = windowOf(d, type);
          return <Meter key={type} label={t("usage.left." + type)} value={pctText(windowLeft(w))} tip={windowTip(t, w, locale)} />;
        })}
        {until === null
          ? <Meter label={t("usage.state")} value={Array.isArray(d.windows) ? t("usage.ok") : "—"} />
          : <Meter label={t("usage.state")} value={t("usage.exhausted")} tip={until ? t("usage.exhaustedUntil", { time: formatShort(until, locale) }) : ""} />}
      </div>
      {monthly && monthlyLeft !== null ? (
        <Bar t={t} label={monthlyLabel} left={monthlyLeft} total={1} value={pctText(monthlyLeft)} />
      ) : plan.name ? (
        <div className="usage-note sub">{plan.name}</div>
      ) : null}
      {plan.period_end ? <div className="usage-note sub">{t("usage.periodEnd", { time: formatShort(plan.period_end, locale) })}</div> : null}
      {until !== null ? (
        <div className="usage-note warn-text">{until ? t("usage.exhaustedUntil", { time: formatShort(until, locale) }) : t("usage.exhausted")}</div>
      ) : null}
      {d.plan_error ? <div className="usage-note sub">{t("usage.planFailed", { msg: d.plan_error })}</div> : null}
      {d.error ? <div className="usage-note danger-text">{t("usage.failed", { msg: d.error })}</div> : null}
      {q && q.state === "error" ? <div className="usage-note danger-text">{t("usage.failed", { msg: q.error })}</div> : null}
    </>
  );
}

function ModelCells({ m }: { t: T; m: ModelView }) {
  const tags = Array.isArray(m.tags) ? m.tags : [];
  return (
    <>
      <td>
        <div className="mono strong">{m.id}</div>
        <div className="sub mono">{m.upstream_id}</div>
      </td>
      <td>
        <div>{m.description || "—"}</div>
        {tags.length ? <div className="sub">{tags.join(" · ")}</div> : null}
      </td>
    </>
  );
}

export const components: ProviderComponents = {
  KeySummary,
  KeyUsage,
  modelColumns: ["col.model", "col.description"],
  ModelCells,
};
