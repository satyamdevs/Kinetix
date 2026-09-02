"use client";

import { useCallback, useEffect, useState } from "react";
import {
  createKey,
  deleteKey,
  listKeys,
  setKeyEnabled,
  type ApiKey,
  type CreateKeyInput,
} from "@/lib/keys";

const EMPTY_FORM: CreateKeyInput = {
  name: "",
  owner: "",
  requests_per_minute: 0,
  tokens_per_month: 0,
  ttl_hours: 0,
};

export default function PoliciesPage() {
  const [keys, setKeys] = useState<ApiKey[]>([]);
  const [form, setForm] = useState<CreateKeyInput>(EMPTY_FORM);
  const [secret, setSecret] = useState<string | null>(null);
  const [error, setError] = useState<string | null>(null);
  const [creating, setCreating] = useState(false);
  const [busy, setBusy] = useState<string | null>(null);
  const [loaded, setLoaded] = useState(false);

  const refresh = useCallback(async () => {
    try {
      const data = await listKeys();
      setKeys(data.keys);
      setError(null);
    } catch (e) {
      setError(e instanceof Error ? e.message : "Failed to load keys");
    } finally {
      setLoaded(true);
    }
  }, []);

  useEffect(() => {
    void refresh();
  }, [refresh]);

  const onField = (field: keyof CreateKeyInput, value: string | number) => {
    setForm((f) => ({ ...f, [field]: value }));
  };

  const onCreate = async (e: React.FormEvent) => {
    e.preventDefault();
    setCreating(true);
    setSecret(null);
    setError(null);
    try {
      const created = await createKey(form);
      setSecret(created.secret);
      setForm(EMPTY_FORM);
      await refresh();
    } catch (err) {
      setError(err instanceof Error ? err.message : "Create failed");
    } finally {
      setCreating(false);
    }
  };

  const onToggle = async (key: ApiKey) => {
    setBusy(key.id);
    setError(null);
    try {
      await setKeyEnabled(key.id, !key.enabled);
      await refresh();
    } catch (err) {
      setError(err instanceof Error ? err.message : "Toggle failed");
    } finally {
      setBusy(null);
    }
  };

  const onDelete = async (key: ApiKey) => {
    if (!window.confirm(`Revoke key "${key.name}" (${key.prefix})? This cannot be undone.`)) {
      return;
    }
    setBusy(key.id);
    setError(null);
    try {
      await deleteKey(key.id);
      await refresh();
    } catch (err) {
      setError(err instanceof Error ? err.message : "Delete failed");
    } finally {
      setBusy(null);
    }
  };

  return (
    <>
      <div className="card">
        <h2>Create API key</h2>
        <form className="policy-form" onSubmit={onCreate}>
          <label>
            Name <span className="req">*</span>
            <input
              value={form.name}
              onChange={(e) => onField("name", e.target.value)}
              placeholder="my-app"
              required
            />
          </label>
          <label>
            Owner
            <input
              value={form.owner}
              onChange={(e) => onField("owner", e.target.value)}
              placeholder="team-a"
            />
          </label>
          <label>
            Requests / min (0 = unlimited)
            <input
              type="number"
              min={0}
              value={form.requests_per_minute}
              onChange={(e) => onField("requests_per_minute", Number(e.target.value))}
            />
          </label>
          <label>
            Tokens / month (0 = unlimited)
            <input
              type="number"
              min={0}
              value={form.tokens_per_month}
              onChange={(e) => onField("tokens_per_month", Number(e.target.value))}
            />
          </label>
          <label>
            TTL (hours, 0 = never)
            <input
              type="number"
              min={0}
              value={form.ttl_hours}
              onChange={(e) => onField("ttl_hours", Number(e.target.value))}
            />
          </label>
          <div className="form-actions">
            <button type="submit" disabled={creating}>
              {creating ? "Creating…" : "Create key"}
            </button>
          </div>
        </form>

        {secret && (
          <div className="secret">
            <strong>Key created — copy it now, it won&apos;t be shown again:</strong>
            <code>{secret}</code>
          </div>
        )}
      </div>

      {error && <div className="error">{error}</div>}

      <div className="card">
        <h2>API keys & policies</h2>
        {!loaded ? (
          <div className="empty">Loading…</div>
        ) : keys.length === 0 ? (
          <div className="empty">
            No keys yet. Create one above to define its policy.
          </div>
        ) : (
          <table>
            <thead>
              <tr>
                <th>Name</th>
                <th>Owner</th>
                <th>Prefix</th>
                <th>State</th>
                <th>RPM</th>
                <th>TPM</th>
                <th>Actions</th>
              </tr>
            </thead>
            <tbody>
              {keys.map((k) => {
                const isAdmin = k.scopes.includes("admin");
                return (
                  <tr key={k.id} className={k.enabled ? "" : "disabled-row"}>
                    <td>
                      {k.name}
                      {isAdmin && <span className="badge">admin</span>}
                    </td>
                    <td>{k.owner || "—"}</td>
                    <td>
                      <code>{k.prefix}</code>
                    </td>
                    <td>{k.enabled ? "enabled" : "disabled"}</td>
                    <td>{k.limit.requests_per_minute || "∞"}</td>
                    <td>{k.limit.tokens_per_month ? String(k.limit.tokens_per_month) : "∞"}</td>
                    <td className="actions">
                      <button
                        onClick={() => onToggle(k)}
                        disabled={busy === k.id}
                        className={k.enabled ? "danger-soft" : ""}
                      >
                        {k.enabled ? "Disable" : "Enable"}
                      </button>
                      <button
                        onClick={() => onDelete(k)}
                        disabled={busy === k.id || isAdmin}
                        title={
                          isAdmin
                            ? "Revoking the configured admin key would lock you out"
                            : "Revoke permanently"
                        }
                        className="danger"
                      >
                        Revoke
                      </button>
                    </td>
                  </tr>
                );
              })}
            </tbody>
          </table>
        )}
      </div>
    </>
  );
}
