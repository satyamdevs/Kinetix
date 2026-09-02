import { fetchUsageSummary, summarizeDaily, currency, number } from "@/lib/telemetry";

export const dynamic = "force-dynamic";

export default async function UsagePage() {
  let daily:
    | Awaited<ReturnType<typeof summarizeDaily>>
    | null = null;
  let error: string | null = null;

  try {
    const summary = await fetchUsageSummary();
    daily = summarizeDaily(summary.points);
  } catch (e) {
    error = e instanceof Error ? e.message : "Failed to load usage";
  }

  if (error) {
    return (
      <div className="error">
        {error}. Is the gateway running with ORCA_ADMIN_KEY set and a key that
        has the admin scope?
      </div>
    );
  }

  const totalTokens = daily?.reduce((s, p) => s + p.total_tokens, 0) ?? 0;
  const totalCost = daily?.reduce((s, p) => s + p.cost_usd, 0) ?? 0;
  const totalRequests = daily?.reduce((s, p) => s + p.requests, 0) ?? 0;
  const maxTokens = Math.max(...(daily ?? []).map((p) => p.total_tokens), 1);

  return (
    <>
      <div className="grid">
        <div className="stat">
          <div className="label">Requests</div>
          <div className="value">{number(totalRequests)}</div>
        </div>
        <div className="stat">
          <div className="label">Tokens</div>
          <div className="value">{number(totalTokens)}</div>
        </div>
        <div className="stat">
          <div className="label">Cost</div>
          <div className="value">{currency.format(totalCost)}</div>
        </div>
      </div>

      <div className="card">
        <h2>Tokens per day (last 7 days)</h2>
        {!daily || daily.length === 0 ? (
          <div className="empty">No telemetry recorded yet.</div>
        ) : (
          <div className="bars">
            {daily.map((p) => (
              <div
                key={p.day}
                className="bar"
                style={{ height: `${Math.max((p.total_tokens / maxTokens) * 100, 2)}%` }}
                title={`${p.day}: ${number(p.total_tokens)} tokens`}
              />
            ))}
          </div>
        )}
      </div>

      <div className="card">
        <h2>Daily totals</h2>
        {!daily || daily.length === 0 ? (
          <div className="empty">No data</div>
        ) : (
          <table>
            <thead>
              <tr>
                <th>Day</th>
                <th>Requests</th>
                <th>Tokens</th>
                <th>Cost</th>
              </tr>
            </thead>
            <tbody>
              {daily.map((p) => (
                <tr key={p.day}>
                  <td>{p.day}</td>
                  <td>{number(p.requests)}</td>
                  <td>{number(p.total_tokens)}</td>
                  <td>{currency.format(p.cost_usd)}</td>
                </tr>
              ))}
            </tbody>
          </table>
        )}
      </div>
    </>
  );
}