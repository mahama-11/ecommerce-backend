package platform

import (
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func TestClientContractCoversPrimaryPlatformDependencies(t *testing.T) {
	var internalRequests int
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.HasPrefix(r.URL.Path, "/internal/v1/") {
			internalRequests++
			if r.Header.Get("X-Internal-Service") != "v-ecommerce-backend-test" {
				t.Fatalf("missing internal service header for %s", r.URL.String())
			}
			if r.Header.Get("X-Internal-Service-Secret") != "test-internal-secret" {
				t.Fatalf("missing internal service secret for %s", r.URL.String())
			}
			if r.Header.Get("X-Internal-Signature") == "" || r.Header.Get("X-Internal-Timestamp") == "" || r.Header.Get("X-Request-ID") == "" || r.Header.Get("X-Trace-ID") == "" {
				t.Fatalf("missing signed internal headers for %s", r.URL.String())
			}
		}

		switch {
		case r.Method == http.MethodPost && r.URL.Path == "/api/v1/auth/register":
			if r.Header.Get("X-Internal-Service-Secret") != "" {
				t.Fatalf("public register must not send internal service secret")
			}
			writeEnvelope(t, w, http.StatusOK, 0, authResultData("register-token"), "", "", "")
		case r.Method == http.MethodPost && r.URL.Path == "/api/v1/auth/login":
			if r.Header.Get("X-Internal-Service-Secret") != "" {
				t.Fatalf("public login must not send internal service secret")
			}
			writeEnvelope(t, w, http.StatusOK, 0, authResultData("login-token"), "", "", "")
		case r.Method == http.MethodGet && r.URL.Path == "/internal/v1/users/user-1/profile":
			if r.URL.Query().Get("org_id") != "org-1" {
				t.Fatalf("profile org_id query = %q", r.URL.Query().Get("org_id"))
			}
			writeEnvelope(t, w, http.StatusOK, 0, userProfileData(), "", "", "")
		case r.Method == http.MethodGet && r.URL.Path == "/internal/v1/access/users/user-1/orgs/org-1":
			writeEnvelope(t, w, http.StatusOK, 0, map[string]any{"user_id": "user-1", "org_id": "org-1", "org_role": "owner", "permissions": []string{"platform.access"}}, "", "", "")
		case r.Method == http.MethodGet && r.URL.Path == "/internal/v1/wallet/summary":
			assertQuery(t, r, "billing_subject_type", "organization")
			assertQuery(t, r, "billing_subject_id", "org-1")
			assertQuery(t, r, "product_code", "ecommerce")
			writeEnvelope(t, w, http.StatusOK, 0, walletSummaryData(), "", "", "")
		case r.Method == http.MethodGet && r.URL.Path == "/internal/v1/controls/quota/balance":
			assertQuery(t, r, "billable_item_code", "image_generation")
			writeEnvelope(t, w, http.StatusOK, 0, map[string]any{"billing_subject_type": "organization", "billing_subject_id": "org-1", "billable_item_code": "image_generation", "granted": 10, "consumed": 2, "reserved": 1, "available": 7}, "", "", "")
		case r.Method == http.MethodGet && r.URL.Path == "/internal/v1/catalog/offerings":
			assertQuery(t, r, "product_code", "ecommerce")
			writeEnvelope(t, w, http.StatusOK, 0, offeringsData(), "", "", "")
		case r.Method == http.MethodGet && r.URL.Path == "/internal/v1/wallet/allowances/policy-1":
			writeEnvelope(t, w, http.StatusOK, 0, map[string]any{"id": "policy-1", "product_code": "ecommerce", "asset_code": "ECOMMERCE_CREDIT", "amount": 100, "status": "active"}, "", "", "")
		case r.Method == http.MethodPost && r.URL.Path == "/internal/v1/wallet/ledger":
			writeEnvelope(t, w, http.StatusOK, 0, map[string]any{"account": map[string]any{"id": "acct-1", "balance": 10}, "bucket": map[string]any{"id": "bucket-1", "balance": 10}, "ledger": map[string]any{"id": "ledger-1", "amount": 10, "direction": "credit"}}, "", "", "")
		case r.Method == http.MethodGet && r.URL.Path == "/internal/v1/wallet/ledger":
			writeEnvelope(t, w, http.StatusOK, 0, itemsData(map[string]any{"id": "ledger-1", "amount": 10, "direction": "credit"}), "", "", "")
		case r.Method == http.MethodGet && r.URL.Path == "/internal/v1/wallet/accounts":
			writeEnvelope(t, w, http.StatusOK, 0, itemsData(map[string]any{"id": "acct-1", "balance": 10, "asset_code": "ECOMMERCE_CREDIT"}), "", "", "")
		case r.Method == http.MethodPost && r.URL.Path == "/internal/v1/wallet/cycle-allowances":
			writeEnvelope(t, w, http.StatusOK, 0, map[string]any{"account": map[string]any{"id": "acct-1", "balance": 20}, "bucket": map[string]any{"id": "bucket-1", "balance": 20}}, "", "", "")
		case r.Method == http.MethodGet && r.URL.Path == "/internal/v1/wallet/assets":
			writeEnvelope(t, w, http.StatusOK, 0, itemsData(map[string]any{"asset_code": "ECOMMERCE_CREDIT", "product_code": "ecommerce", "asset_type": "credit", "status": "active"}), "", "", "")
		case r.Method == http.MethodPost && r.URL.Path == "/internal/v1/wallet/assets":
			writeEnvelope(t, w, http.StatusOK, 0, map[string]any{"asset_code": "ECOMMERCE_CREDIT", "product_code": "ecommerce", "asset_type": "credit", "status": "active"}, "", "", "")
		case r.Method == http.MethodGet && r.URL.Path == "/internal/v1/controls/quota/policies":
			writeEnvelope(t, w, http.StatusOK, 0, itemsData(map[string]any{"id": "quota-policy-1", "product_code": "ecommerce", "package_code": "starter", "billable_item_code": "image_generation", "units": 100, "status": "active"}), "", "", "")
		case r.Method == http.MethodPost && r.URL.Path == "/internal/v1/controls/quota/grants":
			writeEnvelope(t, w, http.StatusOK, 0, map[string]any{"id": "grant-1"}, "", "", "")
		case r.Method == http.MethodGet && r.URL.Path == "/internal/v1/incentives/rewards":
			writeEnvelope(t, w, http.StatusOK, 0, itemsData(map[string]any{"id": "reward-1", "product_code": "ecommerce", "amount": 5, "status": "issued"}), "", "", "")
		case r.Method == http.MethodGet && r.URL.Path == "/internal/v1/incentives/commissions":
			writeEnvelope(t, w, http.StatusOK, 0, itemsData(map[string]any{"id": "commission-1", "product_code": "ecommerce", "amount": 7, "status": "earned"}), "", "", "")
		case r.Method == http.MethodPost && r.URL.Path == "/internal/v1/incentives/commissions/redeem":
			writeEnvelope(t, w, http.StatusOK, 0, map[string]any{"reward_ledger_id": "reward-2", "asset_code": "ECOMMERCE_CREDIT", "total_amount": 7, "commissions": []map[string]any{{"id": "commission-1", "amount": 7}}}, "", "", "")
		case r.Method == http.MethodGet && r.URL.Path == "/internal/v1/incentives/referral-programs":
			writeEnvelope(t, w, http.StatusOK, 0, itemsData(map[string]any{"id": "program-1", "product_code": "ecommerce", "program_code": "signup", "name": "Signup", "status": "active"}), "", "", "")
		case r.Method == http.MethodPost && r.URL.Path == "/internal/v1/incentives/referral-programs":
			writeEnvelope(t, w, http.StatusOK, 0, map[string]any{"id": "program-2", "product_code": "ecommerce", "program_code": "signup", "name": "Signup", "status": "active"}, "", "", "")
		case r.Method == http.MethodGet && r.URL.Path == "/internal/v1/incentives/referral-codes":
			writeEnvelope(t, w, http.StatusOK, 0, itemsData(map[string]any{"id": "code-1", "program_id": "program-1", "product_code": "ecommerce", "code": "INVITE", "status": "active"}), "", "", "")
		case r.Method == http.MethodPost && r.URL.Path == "/internal/v1/incentives/referral-codes":
			writeEnvelope(t, w, http.StatusOK, 0, map[string]any{"id": "code-2", "program_id": "program-1", "product_code": "ecommerce", "code": "INVITE2", "status": "active"}, "", "", "")
		case r.Method == http.MethodGet && r.URL.Path == "/internal/v1/incentives/referral-codes/INVITE/resolve":
			writeEnvelope(t, w, http.StatusOK, 0, map[string]any{"code": "INVITE", "product_code": "ecommerce", "program_id": "program-1", "program_code": "signup", "program_name": "Signup", "trigger_type": "signup", "status": "active"}, "", "", "")
		case r.Method == http.MethodGet && r.URL.Path == "/internal/v1/incentives/referral-conversions":
			writeEnvelope(t, w, http.StatusOK, 0, itemsData(map[string]any{"id": "conversion-1", "product_code": "ecommerce", "status": "tracked"}), "", "", "")
		case r.Method == http.MethodPost && r.URL.Path == "/internal/v1/incentives/referral-conversions":
			writeEnvelope(t, w, http.StatusOK, 0, map[string]any{"id": "conversion-2", "product_code": "ecommerce", "status": "tracked"}, "", "", "")
		case r.Method == http.MethodGet && r.URL.Path == "/internal/v1/incentives/channel-partners":
			writeEnvelope(t, w, http.StatusOK, 0, itemsData(map[string]any{"id": "partner-1", "code": "chan", "name": "Channel", "status": "active"}), "", "", "")
		case r.Method == http.MethodGet && r.URL.Path == "/internal/v1/incentives/channel-programs":
			writeEnvelope(t, w, http.StatusOK, 0, itemsData(map[string]any{"id": "channel-program-1", "product_code": "ecommerce", "program_code": "affiliate", "name": "Affiliate", "status": "active"}), "", "", "")
		case r.Method == http.MethodGet && r.URL.Path == "/internal/v1/incentives/channel-bindings":
			writeEnvelope(t, w, http.StatusOK, 0, itemsData(map[string]any{"id": "binding-1", "product_code": "ecommerce", "org_id": "org-1", "channel_partner_id": "partner-1", "status": "active"}), "", "", "")
		case r.Method == http.MethodGet && r.URL.Path == "/internal/v1/incentives/channel-commissions":
			writeEnvelope(t, w, http.StatusOK, 0, itemsData(map[string]any{"id": "channel-commission-1", "product_code": "ecommerce", "channel_partner_id": "partner-1", "commission_amount": 30, "status": "available"}), "", "", "")
		case r.Method == http.MethodGet && r.URL.Path == "/internal/v1/incentives/channel-settlement-batches":
			writeEnvelope(t, w, http.StatusOK, 0, itemsData(map[string]any{"id": "batch-1", "batch_no": "B1", "product_code": "ecommerce", "status": "open"}), "", "", "")
		case r.Method == http.MethodGet && r.URL.Path == "/internal/v1/incentives/channel-settlement-batches/batch-1":
			detail := map[string]any{
				"batch": map[string]any{"id": "batch-1", "batch_no": "B1", "product_code": "ecommerce"},
				"items": []map[string]any{
					{
						"item":                  map[string]any{"id": "item-1", "currency": "USD"},
						"commission_ledger_ids": []string{"channel-commission-1"},
					},
				},
			}
			writeEnvelope(t, w, http.StatusOK, 0, detail, "", "", "")
		case r.Method == http.MethodPost && r.URL.Path == "/internal/v1/incentives/channel-events/charges":
			writeEnvelope(t, w, http.StatusOK, 0, map[string]any{"matched": true, "idempotent": false, "status": "recorded", "binding_id": "binding-1"}, "", "", "")
		case r.Method == http.MethodPost && r.URL.Path == "/internal/v1/incentives/channel-events/refunds":
			writeEnvelope(t, w, http.StatusOK, 0, map[string]any{"matched": true, "idempotent": false, "action": "reversed"}, "", "", "")
		case r.Method == http.MethodGet && r.URL.Path == "/internal/v1/template-ops/catalog":
			assertQuery(t, r, "product_code", "ecommerce")
			assertQuery(t, r, "tool_slug", "listing")
			writeEnvelope(t, w, http.StatusOK, 0, map[string]any{"items": []map[string]any{{"template_ref": "tpl-1", "product_code": "ecommerce", "template_id": "tpl-1", "slug": "listing", "name": "Listing", "status": "published"}}, "total": 1, "limit": 20, "offset": 0}, "", "", "")
		case r.Method == http.MethodGet && r.URL.Path == "/internal/v1/template-ops/catalog/tpl-1":
			writeEnvelope(t, w, http.StatusOK, 0, map[string]any{"item": map[string]any{"template_ref": "tpl-1", "product_code": "ecommerce", "template_id": "tpl-1", "slug": "listing", "name": "Listing", "status": "published"}, "product": "ecommerce"}, "", "", "")
		case r.Method == http.MethodGet && r.URL.Path == "/internal/v1/storage/assets/content":
			assertQuery(t, r, "storage_key", "asset/key.png")
			w.Header().Set("Content-Type", "image/png")
			w.WriteHeader(http.StatusOK)
			_, _ = w.Write([]byte("asset-bytes"))
		default:
			t.Fatalf("unexpected request %s %s", r.Method, r.URL.String())
		}
	}))
	defer server.Close()

	client := newTestClient(server)
	if client.BaseURL() != server.URL {
		t.Fatalf("BaseURL() = %q", client.BaseURL())
	}
	if out, err := client.Register(AuthRegisterInput{FullName: "Merchant", Email: "merchant@example.com", Password: "secret123", Company: "Merchant Co"}); err != nil || out.AccessToken != "register-token" {
		t.Fatalf("Register = %+v, %v", out, err)
	}
	if out, err := client.Login(AuthLoginInput{Email: "merchant@example.com", Password: "secret123"}); err != nil || out.AccessToken != "login-token" {
		t.Fatalf("Login = %+v, %v", out, err)
	}
	if out, err := client.GetUserProfile("user-1", "org-1"); err != nil || out.ID != "user-1" {
		t.Fatalf("GetUserProfile = %+v, %v", out, err)
	}
	if out, err := client.GetAccessContext("user-1", "org-1"); err != nil || out.OrgRole != "owner" {
		t.Fatalf("GetAccessContext = %+v, %v", out, err)
	}
	if out, err := client.GetWalletSummary("organization", "org-1", "ecommerce"); err != nil || out.TotalBalance != 100 {
		t.Fatalf("GetWalletSummary = %+v, %v", out, err)
	}
	if out, err := client.GetQuotaBalance("organization", "org-1", "image_generation"); err != nil || out.Available != 7 {
		t.Fatalf("GetQuotaBalance = %+v, %v", out, err)
	}
	if out, err := client.GetCatalogOfferings("ecommerce"); err != nil || len(out.SKUs) != 1 {
		t.Fatalf("GetCatalogOfferings = %+v, %v", out, err)
	}
	if out, err := client.GetAllowancePolicy("policy-1"); err != nil || out.ID != "policy-1" {
		t.Fatalf("GetAllowancePolicy = %+v, %v", out, err)
	}
	if acct, bucket, ledger, err := client.PostWalletLedger(PostWalletLedgerInput{BillingSubjectType: "organization", BillingSubjectID: "org-1", AssetCode: "ECOMMERCE_CREDIT", Direction: "credit", Amount: 10}); err != nil || acct.ID == "" || bucket.ID == "" || ledger.ID == "" {
		t.Fatalf("PostWalletLedger = %+v %+v %+v, %v", acct, bucket, ledger, err)
	}
	if bucket, acct, err := client.GrantCycleAllowance(GrantCycleAllowanceInput{BillingSubjectType: "organization", BillingSubjectID: "org-1", AssetCode: "ECOMMERCE_CREDIT", CycleKey: "2026-06", Amount: 20}); err != nil || bucket.ID == "" || acct.ID == "" {
		t.Fatalf("GrantCycleAllowance = %+v %+v, %v", bucket, acct, err)
	}
	if out, err := client.ListWalletAccounts("organization", "org-1", "ecommerce"); err != nil || len(out) != 1 {
		t.Fatalf("ListWalletAccounts = %+v, %v", out, err)
	}
	if out, err := client.ListWalletLedger("acct-1", "ecommerce"); err != nil || len(out) != 1 {
		t.Fatalf("ListWalletLedger = %+v, %v", out, err)
	}
	if out, err := client.ListAssetDefinitions("ecommerce", "", "active"); err != nil || len(out) != 1 {
		t.Fatalf("ListAssetDefinitions = %+v, %v", out, err)
	}
	if out, err := client.CreateAssetDefinition(CreateAssetDefinitionInput{AssetCode: "ECOMMERCE_CREDIT", ProductCode: "ecommerce", AssetType: "credit"}); err != nil || out.AssetCode != "ECOMMERCE_CREDIT" {
		t.Fatalf("CreateAssetDefinition = %+v, %v", out, err)
	}
	if out, err := client.ListQuotaGrantPolicies("ecommerce", "starter"); err != nil || len(out) != 1 {
		t.Fatalf("ListQuotaGrantPolicies = %+v, %v", out, err)
	}
	if err := client.GrantQuota(GrantQuotaInput{BillingSubjectType: "organization", BillingSubjectID: "org-1", BillableItemCode: "image_generation", Units: 5}); err != nil {
		t.Fatalf("GrantQuota = %v", err)
	}
	if out, err := client.ListRewards("ecommerce", "organization", "org-1"); err != nil || len(out) != 1 {
		t.Fatalf("ListRewards = %+v, %v", out, err)
	}
	if out, err := client.ListCommissions("ecommerce", "organization", "org-1", "earned"); err != nil || len(out) != 1 {
		t.Fatalf("ListCommissions = %+v, %v", out, err)
	}
	if out, err := client.RedeemCommissions(RedeemCommissionsInput{ProductCode: "ecommerce", BeneficiarySubjectType: "organization", BeneficiarySubjectID: "org-1", AssetCode: "ECOMMERCE_CREDIT"}); err != nil || out.TotalAmount != 7 {
		t.Fatalf("RedeemCommissions = %+v, %v", out, err)
	}
	if out, err := client.ListReferralPrograms("ecommerce", "active"); err != nil || len(out) != 1 {
		t.Fatalf("ListReferralPrograms = %+v, %v", out, err)
	}
	if out, err := client.CreateReferralProgram(CreateReferralProgramInput{ProductCode: "ecommerce", ProgramCode: "signup", Name: "Signup", TriggerType: "signup", CommissionPolicy: "fixed_amount"}); err != nil || out.ID != "program-2" {
		t.Fatalf("CreateReferralProgram = %+v, %v", out, err)
	}
	if out, err := client.ListReferralCodes("program-1", "organization", "org-1", "active"); err != nil || len(out) != 1 {
		t.Fatalf("ListReferralCodes = %+v, %v", out, err)
	}
	if out, err := client.CreateReferralCode(CreateReferralCodeInput{ProgramCode: "signup", Code: "INVITE2", PromoterSubjectType: "organization", PromoterSubjectID: "org-1"}); err != nil || out.Code != "INVITE2" {
		t.Fatalf("CreateReferralCode = %+v, %v", out, err)
	}
	if out, err := client.ResolveReferralCode("INVITE", "ecommerce"); err != nil || out.Code != "INVITE" {
		t.Fatalf("ResolveReferralCode = %+v, %v", out, err)
	}
	if out, err := client.ListReferralConversions("ecommerce", "organization", "org-1", "tracked"); err != nil || len(out) != 1 {
		t.Fatalf("ListReferralConversions = %+v, %v", out, err)
	}
	if out, err := client.CreateReferralConversion(CreateReferralConversionInput{ReferralCode: "INVITE", ProductCode: "ecommerce", TriggerType: "signup", ReferredSubjectType: "organization", ReferredSubjectID: "org-1", ReferenceType: "auth_register", ReferenceID: "org-1"}); err != nil || out.ID != "conversion-2" {
		t.Fatalf("CreateReferralConversion = %+v, %v", out, err)
	}
	if out, err := client.ListChannelPartners("active"); err != nil || len(out) != 1 {
		t.Fatalf("ListChannelPartners = %+v, %v", out, err)
	}
	if out, err := client.ListChannelPrograms("ecommerce", "active"); err != nil || len(out) != 1 {
		t.Fatalf("ListChannelPrograms = %+v, %v", out, err)
	}
	if out, err := client.ListChannelBindings("ecommerce", "org-1", "active"); err != nil || len(out) != 1 {
		t.Fatalf("ListChannelBindings = %+v, %v", out, err)
	}
	if out, err := client.ListChannelCommissions("ecommerce", "partner-1", "available"); err != nil || len(out) != 1 {
		t.Fatalf("ListChannelCommissions = %+v, %v", out, err)
	}
	if out, err := client.ListChannelSettlementBatches("ecommerce", "channel-program-1", "open"); err != nil || len(out) != 1 {
		t.Fatalf("ListChannelSettlementBatches = %+v, %v", out, err)
	}
	if out, err := client.GetChannelSettlementBatch("batch-1"); err != nil || out.Batch.ID != "batch-1" || len(out.Items) != 1 {
		t.Fatalf("GetChannelSettlementBatch = %+v, %v", out, err)
	}
	if out, err := client.RecordChannelCharge(RecordChannelChargeInput{EventID: "event-1", ProductCode: "ecommerce", OrgID: "org-1", AppliesTo: "order", SourceChargeID: "charge-1", GrossAmount: 100}); err != nil || !out.Matched {
		t.Fatalf("RecordChannelCharge = %+v, %v", out, err)
	}
	if out, err := client.RecordChannelRefund(RecordChannelRefundInput{EventID: "refund-1", ProductCode: "ecommerce", SourceChargeID: "charge-1", RefundAmount: 10, RefundType: "partial"}); err != nil || out.Action != "reversed" {
		t.Fatalf("RecordChannelRefund = %+v, %v", out, err)
	}
	if out, err := client.InternalTemplateCatalog(InternalTemplateCatalogInput{ProductCode: "ecommerce", ToolSlug: "listing", Limit: 20, PublishedOnly: true}); err != nil || out.Total != 1 {
		t.Fatalf("InternalTemplateCatalog = %+v, %v", out, err)
	}
	if out, err := client.InternalTemplateCatalogDetail("tpl-1"); err != nil || out.Item.TemplateRef != "tpl-1" {
		t.Fatalf("InternalTemplateCatalogDetail = %+v, %v", out, err)
	}
	body, headers, err := client.DownloadAsset("asset/key.png")
	if err != nil {
		t.Fatalf("DownloadAsset returned error: %v", err)
	}
	defer body.Close()
	payload, _ := io.ReadAll(body)
	if string(payload) != "asset-bytes" || headers.Get("Content-Type") != "image/png" {
		t.Fatalf("unexpected asset download: content_type=%s payload=%q", headers.Get("Content-Type"), payload)
	}
	if internalRequests == 0 {
		t.Fatalf("expected internal contract requests")
	}
}

func TestClientMapsRepresentativePlatformHTTPStatuses(t *testing.T) {
	cases := []struct {
		status       int
		code         int
		errorCode    string
		wantConflict bool
		wantUnauth   bool
		wantNotFound bool
		wantBadReq   bool
		wantTooLarge bool
	}{
		{status: http.StatusBadRequest, code: 400, errorCode: "BAD_REQUEST", wantBadReq: true},
		{status: http.StatusUnauthorized, code: 401, errorCode: "UNAUTHORIZED", wantUnauth: true},
		{status: http.StatusForbidden, code: 403, errorCode: "FORBIDDEN"},
		{status: http.StatusNotFound, code: 404, errorCode: "NOT_FOUND", wantNotFound: true},
		{status: http.StatusConflict, code: 409, errorCode: "CONFLICT", wantConflict: true},
		{status: http.StatusRequestEntityTooLarge, code: 413, errorCode: "PAYLOAD_TOO_LARGE", wantTooLarge: true},
		{status: http.StatusInternalServerError, code: 500, errorCode: "PLATFORM_INTERNAL"},
	}
	for _, tc := range cases {
		t.Run(tc.errorCode, func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				writeEnvelope(t, w, tc.status, tc.code, nil, "upstream failure", tc.errorCode, "stable hint")
			}))
			defer server.Close()
			_, err := newTestClient(server).GetAccessContext("user-1", "org-1")
			if err == nil {
				t.Fatalf("expected platform error")
			}
			if StatusCode(err) != tc.status || ErrorCode(err) != tc.errorCode || ErrorHint(err) != "stable hint" {
				t.Fatalf("unexpected platform error semantics: status=%d code=%q hint=%q err=%v", StatusCode(err), ErrorCode(err), ErrorHint(err), err)
			}
			if IsConflict(err) != tc.wantConflict || IsUnauthorized(err) != tc.wantUnauth || IsNotFound(err) != tc.wantNotFound || IsBadRequest(err) != tc.wantBadReq || IsPayloadTooLarge(err) != tc.wantTooLarge {
				t.Fatalf("helper classification mismatch for %v", err)
			}
			if strings.Contains(err.Error(), "test-internal-secret") {
				t.Fatalf("platform error leaked internal secret: %s", err.Error())
			}
		})
	}
}

func TestClientInternalPostErrorPreservesSemanticEnvelope(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		writeEnvelope(t, w, http.StatusConflict, 409, nil, "quota duplicate", "QUOTA_ALREADY_GRANTED", "Use the existing grant.")
	}))
	defer server.Close()

	err := newTestClient(server).GrantQuota(GrantQuotaInput{BillingSubjectType: "organization", BillingSubjectID: "org-1", BillableItemCode: "image_generation", Units: 1})
	if err == nil || !IsConflict(err) || ErrorCode(err) != "QUOTA_ALREADY_GRANTED" || strings.Contains(err.Error(), "test-internal-secret") {
		t.Fatalf("unexpected internal post error semantics: %v", err)
	}
}

func TestClientDownloadAssetErrorDoesNotExposeRequestSecret(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("X-Internal-Service-Secret") != "test-internal-secret" {
			t.Fatalf("download missing internal service secret")
		}
		w.WriteHeader(http.StatusNotFound)
		_, _ = w.Write([]byte("asset missing"))
	}))
	defer server.Close()

	body, _, err := newTestClient(server).DownloadAsset("missing/key.png")
	if body != nil || err == nil {
		t.Fatalf("expected download error, body=%v err=%v", body, err)
	}
	if strings.Contains(err.Error(), "test-internal-secret") {
		t.Fatalf("download error leaked internal secret: %s", err.Error())
	}
}

func authResultData(token string) map[string]any {
	return map[string]any{"access_token": token, "user": userProfileData()}
}

func userProfileData() map[string]any {
	return map[string]any{"id": "user-1", "email": "merchant@example.com", "full_name": "Merchant", "avatar_url": "https://cdn.example/avatar.png", "role": "user", "org_role": "owner", "org_id": "org-1", "last_active_org_id": "org-1", "plan_id": "starter", "status": "active", "permissions": []string{"platform.access"}, "orgs": []map[string]any{{"id": "org-1", "name": "Merchant Workspace", "role": "owner"}}}
}

func walletSummaryData() map[string]any {
	return map[string]any{"billing_subject_type": "organization", "billing_subject_id": "org-1", "product_code": "ecommerce", "total_balance": 100, "permanent_balance": 80, "reward_balance": 20, "allowance_balance": 0, "assets": []map[string]any{{"asset_code": "ECOMMERCE_CREDIT", "asset_type": "credit", "lifecycle_type": "permanent", "account_balance": 100, "available_balance": 100}}}
}

func offeringsData() map[string]any {
	now := time.Now().UTC().Format(time.RFC3339)
	return map[string]any{
		"product":            map[string]any{"id": "product-1", "code": "ecommerce", "name": "Agent Ecommerce", "status": "active", "created_at": now, "updated_at": now},
		"skus":               []map[string]any{{"id": "sku-1", "product_id": "product-1", "code": "starter", "name": "Starter", "sku_type": "package", "billing_mode": "prepaid", "currency": "USD", "list_price": 9900, "status": "active", "created_at": now, "updated_at": now}},
		"packages":           []map[string]any{{"id": "package-1", "product_id": "product-1", "code": "starter", "name": "Starter Package", "package_type": "credits", "status": "active", "created_at": now, "updated_at": now}},
		"billable_items":     []map[string]any{{"id": "item-1", "product_id": "product-1", "code": "image_generation", "name": "Image generation", "meter_unit": "image", "billing_scope": "organization", "settlement_mode": "prepaid", "pricing_behavior": "unit", "status": "active", "created_at": now, "updated_at": now}},
		"rate_cards":         []map[string]any{{"id": "rate-1", "product_id": "product-1", "code": "starter-rate", "target_type": "sku", "target_id": "sku-1", "price_model": "flat", "currency": "USD", "version": 1, "status": "active", "created_at": now, "updated_at": now}},
		"asset_definitions":  []map[string]any{{"asset_code": "ECOMMERCE_CREDIT", "product_code": "ecommerce", "asset_type": "credit", "lifecycle_type": "permanent", "status": "active", "created_at": now, "updated_at": now}},
		"allowance_policies": []map[string]any{{"id": "policy-1", "product_code": "ecommerce", "billing_subject_type": "organization", "billing_subject_id": "org-1", "asset_code": "ECOMMERCE_CREDIT", "amount": 100, "status": "active", "created_at": now, "updated_at": now}},
	}
}

func itemsData(item map[string]any) map[string]any {
	return map[string]any{"items": []map[string]any{item}}
}

func assertQuery(t *testing.T, r *http.Request, key, want string) {
	t.Helper()
	if got := r.URL.Query().Get(key); got != want {
		t.Fatalf("query %s = %q, want %q for %s", key, got, want, r.URL.String())
	}
}
