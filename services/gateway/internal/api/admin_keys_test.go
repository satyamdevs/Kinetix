package api_test

import (
	"encoding/json"
	"net/http"
	"strings"
	"testing"

	"orca/gateway/internal/api"
	"orca/gateway/internal/keys"
	"orca/gateway/internal/limits"
	"orca/gateway/internal/provider"
)

// adminHandler builds a router with an admin key in the store plus a Keeper
// that the admin endpoints operate on.
func adminHandler(t *testing.T) (http.Handler, *keys.Memory, string) {
	t.Helper()
	mem := keys.NewMemory()

	adminRaw, adminKey, _ := keys.Generate("admin", "ops", keys.Limit{}, 0)
	adminKey.Scopes = []string{"admin"}
	mem.Upsert(adminKey)

	gov := &api.Governance{Keys: mem, Keeper: mem, Limiter: limits.NewMemory()}
	r := provider.NewRegistry()
	r.Register("fake", fakeStreamer{})
	return api.NewRouter(gov, r), mem, adminRaw
}

func TestKeyAdminRequiresAdminScope(t *testing.T) {
	h, mem, _ := adminHandler(t)
	userRaw, userKey, _ := keys.Generate("user", "dev", keys.Limit{}, 0)
	mem.Upsert(userKey)

	// A valid key without the admin scope must be rejected from /admin routes.
	rec := doAuthReq(t, h, http.MethodGet, "/admin/keys", "", "Bearer "+userRaw)
	if rec.Code != http.StatusForbidden {
		t.Fatalf("non-admin key: status = %d, want 403 (body=%s)", rec.Code, rec.Body.String())
	}
}

func TestKeyAdminCreateListEnableDisableDelete(t *testing.T) {
	h, mem, adminRaw := adminHandler(t)
	auth := "Bearer " + adminRaw

	// List starts with the seeded admin key.
	list := doAuthReq(t, h, http.MethodGet, "/admin/keys", "", auth)
	if list.Code != http.StatusOK {
		t.Fatalf("list: status = %d body=%s", list.Code, list.Body.String())
	}

	// Create a new key, expecting a one-time secret back.
	create := doAuthReq(t, h, http.MethodPost, "/admin/keys",
		`{"name":"my-app","owner":"team-a","requests_per_minute":100,"tokens_per_month":5000}`, auth)
	if create.Code != http.StatusCreated {
		t.Fatalf("create: status = %d body=%s", create.Code, create.Body.String())
	}
	var created struct {
		Key    keys.Key `json:"key"`
		Secret string   `json:"secret"`
	}
	if err := json.Unmarshal(create.Body.Bytes(), &created); err != nil {
		t.Fatal(err)
	}
	if created.Secret == "" || !strings.HasPrefix(created.Secret, keys.KeyPrefix) {
		t.Fatalf("create should return raw secret once: %q", created.Secret)
	}
	if created.Key.ID == "" {
		t.Fatal("create should return key record with id")
	}

	// The raw secret should authenticate against /v1.
	models := doAuthReq(t, h, http.MethodGet, "/v1/models", "", "Bearer "+created.Secret)
	if models.Code != http.StatusOK {
		t.Fatalf("new key auth: status = %d body=%s", models.Code, models.Body.String())
	}

	id := created.Key.ID

	// Disable then enable.
	dis := doAuthReq(t, h, http.MethodPut, "/admin/keys/"+id+"/disable", "", auth)
	if dis.Code != http.StatusOK {
		t.Fatalf("disable: status = %d body=%s", dis.Code, dis.Body.String())
	}
	if rec := doAuthReq(t, h, http.MethodGet, "/v1/models", "", "Bearer "+created.Secret); rec.Code != http.StatusUnauthorized {
		t.Fatalf("disabled key should not authenticate: status = %d", rec.Code)
	}
	en := doAuthReq(t, h, http.MethodPut, "/admin/keys/"+id+"/enable", "", auth)
	if en.Code != http.StatusOK {
		t.Fatalf("enable: status = %d body=%s", en.Code, en.Body.String())
	}
	if rec := doAuthReq(t, h, http.MethodGet, "/v1/models", "", "Bearer "+created.Secret); rec.Code != http.StatusOK {
		t.Fatalf("re-enabled key should authenticate: status = %d", rec.Code)
	}

	// Get single key.
	get := doAuthReq(t, h, http.MethodGet, "/admin/keys/"+id, "", auth)
	if get.Code != http.StatusOK {
		t.Fatalf("get: status = %d body=%s", get.Code, get.Body.String())
	}

	// Delete, then lookup should fail.
	del := doAuthReq(t, h, http.MethodDelete, "/admin/keys/"+id, "", auth)
	if del.Code != http.StatusNoContent {
		t.Fatalf("delete: status = %d body=%s", del.Code, del.Body.String())
	}
	if rec := doAuthReq(t, h, http.MethodGet, "/v1/models", "", "Bearer "+created.Secret); rec.Code != http.StatusUnauthorized {
		t.Fatalf("revoked key should not authenticate: status = %d", rec.Code)
	}
	if _, err := mem.Lookup(t.Context(), created.Key.Hash); err != keys.ErrNotFound {
		t.Fatalf("deleted key should be gone, got err=%v", err)
	}
}

func TestKeyAdminCreateMissingName(t *testing.T) {
	h, _, adminRaw := adminHandler(t)
	rec := doAuthReq(t, h, http.MethodPost, "/admin/keys", `{"owner":"x"}`, "Bearer "+adminRaw)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400 (body=%s)", rec.Code, rec.Body.String())
	}
}

func TestKeyAdminUnknownIDNotFound(t *testing.T) {
	h, _, adminRaw := adminHandler(t)
	rec := doAuthReq(t, h, http.MethodDelete, "/admin/keys/does-not-exist", "", "Bearer "+adminRaw)
	if rec.Code != http.StatusNotFound {
		t.Fatalf("status = %d, want 404 (body=%s)", rec.Code, rec.Body.String())
	}
}
