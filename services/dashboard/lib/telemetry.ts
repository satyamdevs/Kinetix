export type DailyPoint = {
  day: string;
  model: string;
  requests: number;
  prompt_tokens: number;
  completion_tokens: number;
  total_tokens: number;
  cost_usd: number;
  avg_latency_ms: number;
  p95_latency_ms: number;
};

export type UsageSummary = {
  points: DailyPoint[];
};

const GATEWAY_URL = process.env.ORCA_GATEWAY_URL ?? "http://localhost:8080";
const GATEWAY_ADMIN_KEY = process.env.ORCA_ADMIN_KEY ?? "";

/**
 * Fetches the usage summary from the gateway admin API. The admin key is
 * read from the server-side env (ORCA_ADMIN_KEY) and never exposed to the
 * client; this module only runs on the server.
 */
export async function fetchUsageSummary(
  from?: string,
  to?: string,
  model?: string,
): Promise<UsageSummary> {
  const params = new URLSearchParams();
  if (from) params.set("from", from);
  if (to) params.set("to", to);
  if (model) params.set("model", model);

  const url = `${GATEWAY_URL}/admin/usage/summary${
    params.toString() ? `?${params.toString()}` : ""
  }`;

  const res = await fetch(url, {
    headers: {
      Authorization: `Bearer ${GATEWAY_ADMIN_KEY}`,
    },
    cache: "no-store",
  });

  if (!res.ok) {
    throw new Error(`gateway /admin/usage/summary returned ${res.status}`);
  }
  return (await res.json()) as UsageSummary;
}

/** Aggregates per-day totals across all models. */
export function summarizeDaily(points: DailyPoint[]) {
  const byDay = new Map<string, DailyPoint>();
  for (const p of points) {
    const cur = byDay.get(p.day) ?? {
      day: p.day,
      model: "",
      requests: 0,
      prompt_tokens: 0,
      completion_tokens: 0,
      total_tokens: 0,
      cost_usd: 0,
      avg_latency_ms: 0,
      p95_latency_ms: 0,
    };
    cur.requests += p.requests;
    cur.total_tokens += p.total_tokens;
    cur.completion_tokens += p.completion_tokens;
    cur.prompt_tokens += p.prompt_tokens;
    cur.cost_usd += p.cost_usd;
    byDay.set(p.day, cur);
  }
  return Array.from(byDay.values()).sort((a, b) => a.day.localeCompare(b.day));
}

export const currency = new Intl.NumberFormat("en-US", {
  style: "currency",
  currency: "USD",
  maximumFractionDigits: 4,
});

export const number = new Intl.NumberFormat("en-US").format;