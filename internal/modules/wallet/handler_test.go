package wallet

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"ecommerce-service/internal/config"
	"ecommerce-service/internal/middleware"
	"ecommerce-service/internal/platform"

	"github.com/gin-gonic/gin"
)

func TestHandlerSummaryWritesEnvelopeRequestIDAndPlatformFailureStatus(t *testing.T) {
	gin.SetMode(gin.TestMode)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/internal/v1/wallet/summary":
			w.Header().Set("Content-Type", "application/json")
			_ = json.NewEncoder(w).Encode(map[string]any{"code": 0, "data": map[string]any{"billing_subject_type": "organization", "billing_subject_id": "org-handler", "product_code": "ecommerce", "total_balance": 10, "assets": []map[string]any{{"asset_code": "ECOMMERCE_CREDIT"}}}})
		case "/internal/v1/controls/quota/balance":
			w.Header().Set("Content-Type", "application/json")
			_ = json.NewEncoder(w).Encode(map[string]any{"code": 0, "data": map[string]any{"billing_subject_type": "organization", "billing_subject_id": "org-handler", "billable_item_code": "ecommerce.image.generate", "granted": 12, "available": 8}})
		default:
			t.Fatalf("unexpected platform request: %s", r.URL.String())
		}
	}))
	defer server.Close()

	client := platform.New(config.PlatformConfig{BaseURL: server.URL, Timeout: time.Second, InternalServiceSecret: "secret", ServiceName: "wallet-handler-test"})
	h := NewHandler(NewService(client, nil, config.AppConfig{ProductCode: "ecommerce"}))
	r := gin.New()
	r.Use(middleware.RequestContext())
	r.GET("/wallet/summary", func(c *gin.Context) { c.Set("orgID", "org-handler"); h.Summary(c) })

	req := httptest.NewRequest(http.MethodGet, "/wallet/summary", nil)
	req.Header.Set("X-Request-ID", "req-wallet-handler")
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("status = %d body=%s", w.Code, w.Body.String())
	}
	if !strings.Contains(w.Body.String(), `"request_id":"req-wallet-handler"`) || !strings.Contains(w.Body.String(), `"primary_asset_code":"ECOMMERCE_CREDIT"`) {
		t.Fatalf("unexpected response envelope: %s", w.Body.String())
	}
}
