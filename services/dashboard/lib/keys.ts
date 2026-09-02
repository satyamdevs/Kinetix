export type KeyLimit = {
  requests_per_minute: number;
  tokens_per_month: number;
  tokens_per_request: number;
};

export type ApiKey = {
  id: string;
  name: string;
  owner: string;
  prefix: string;
  scopes: string[];
  enabled: boolean;
  limit: KeyLimit;
  created_at: string;
  expires_at?: string | null;
};

export type KeyList = {
  keys: ApiKey[];
};

export type CreateKeyInput = {
  name: string;
  owner: string;
  requests_per_minute: number;
  tokens_per_month: number;
  ttl_hours: number;
};

export type CreatedKey = {
  key: ApiKey;
  secret: string;
};

const GATEWAY_URL = process.env.ORCA_GATEWAY_URL ?? "http://localhost:8080";
const GATEWAY_ADMIN_KEY = process.env.ORCA_ADMIN_KEY ?? "";

async function adminFetch(path: string, init?: RequestInit): Promise<Response> {
  return fetch(`${GATEWAY_URL}${path}`, {
    ...init,
    cache: "no-store",
    headers: {
      Authorization: `Bearer ${GATEWAY_ADMIN_KEY}`,
      "Content-Type": "application/json",
      ...(init?.headers ?? {}),
    },
  });
}

/** Lists all API keys and their policy limits. */
export async function listKeys(): Promise<KeyList> {
  const res = await adminFetch("/admin/keys");
  if (!res.ok) throw new Error(`gateway /admin/keys returned ${res.status}`);
  return (await res.json()) as KeyList;
}

/** Provisions a key and returns the one-time secret. */
export async function createKey(input: CreateKeyInput): Promise<CreatedKey> {
  const res = await adminFetch("/admin/keys", {
    method: "POST",
    body: JSON.stringify(input),
  });
  if (!res.ok) {
    const body = await res.text();
    throw new Error(`create key failed (${res.status}): ${body}`);
  }
  return (await res.json()) as CreatedKey;
}

/** Enables or disables a key by id. */
export async function setKeyEnabled(id: string, enabled: boolean): Promise<void> {
  const res = await adminFetch(`/admin/keys/${id}/${enabled ? "enable" : "disable"}`, {
    method: "PUT",
  });
  if (!res.ok) throw new Error(`set key state failed (${res.status})`);
}

/** Revokes a key permanently by id. */
export async function deleteKey(id: string): Promise<void> {
  const res = await adminFetch(`/admin/keys/${id}`, { method: "DELETE" });
  if (!res.ok) throw new Error(`delete key failed (${res.status})`);
}
