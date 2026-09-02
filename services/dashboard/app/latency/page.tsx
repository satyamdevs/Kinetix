import { fetchUsageSummary, number } from "@/lib/telemetry";

export const dynamic = "force-dynamic";

export default async function LatencyPage() {
  let models: Map<string, { avg: number; p95: number; requests: number }> =
    new Map();
  let error: string | null = null;

  try {
    const summary = await fetchUsageSummary();
    for (const p of summary.points) {
      const cur = models.get(p.model) ?? { avg: 0, p95: 0, requests: 0 };
      // weighted average across days
      const weight = cur.requests + p.requests;
      if (weight === 0) continue;
      cur.avg =
        (cur.avg * cur.requests + p.avg_latency_ms * p.requests) / weight;
      cur.p95 = Math.max(cur.p95, p.p95_latency_ms);
      cur.requests += p.requests;
      models.set(p.model, cur);
    }
  } catch (e) {
    error = e instanceof Error ? e.message : "Failed to load latency";
  }

  if (error) {
    return <div className="error">{error}</div>;
  }

  const rows = Array.from(models.entries()).sort((a, b) =>
    a[0].localeCompare(b[0]),
  );

  return (
    <div className="card">
      <h2>Latency by model (all time, last 7 days)</h2>
      {rows.length === 0 ? (
        <div className="empty">No telemetry recorded yet.</div>
      ) : (
        <table>
          <thead>
            <tr>
              <th>Model</th>
              <th>Requests</th>
              <th>Avg (ms)</th>
              <th>p95 (ms)</th>
            </tr>
          </thead>
          <tbody>
            {rows.map(([model, m]) => (
              <tr key={model}>
                <td>{model}</td>
                <td>{number(m.requests)}</td>
                <td>{m.avg.toFixed(1)}</td>
                <td>{m.p95.toFixed(1)}</td>
              </tr>
            ))}
          </tbody>
        </table>
      )}
    </div>
  );
}