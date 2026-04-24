package workspace

import (
	"testing"

	"ecommerce-service/internal/repository"
)

type fakeRepo struct{}

func TestScopeKey(t *testing.T) {
	key := scopeKey(repository.Scope{UserID: "u1", OrgID: "o1"}, "saved_templates")
	if key != "ecommerce:saved_templates:o1:u1" {
		t.Fatalf("unexpected cache key: %s", key)
	}
}
