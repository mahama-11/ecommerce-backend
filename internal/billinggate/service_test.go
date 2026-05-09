package billinggate

import (
	"encoding/json"
	"testing"

	"ecommerce-service/internal/platform"
)

type fakeGateway struct {
	created  platform.CreateChargeSessionInput
	reserved platform.ReserveInput
	updates  []platform.UpdateChargeSessionInput
	released string
	finalize platform.FinalizeInput
}

func (f *fakeGateway) CreateChargeSession(input platform.CreateChargeSessionInput) (*platform.ChargeSession, error) {
	f.created = input
	return &platform.ChargeSession{ID: "charge-1"}, nil
}
func (f *fakeGateway) UpdateChargeSession(id string, input platform.UpdateChargeSessionInput) (*platform.ChargeSession, error) {
	f.updates = append(f.updates, input)
	return &platform.ChargeSession{ID: id, Status: input.Status}, nil
}
func (f *fakeGateway) ReserveResources(input platform.ReserveInput) (*platform.ResourceReservation, error) {
	f.reserved = input
	return &platform.ResourceReservation{ID: "reservation-1", ReservationKey: input.ReservationKey}, nil
}
func (f *fakeGateway) ReleaseReservation(id string) (*platform.ResourceReservation, error) {
	f.released = id
	return &platform.ResourceReservation{ID: id, Status: "released"}, nil
}
func (f *fakeGateway) FinalizeMetering(input platform.FinalizeInput) (*platform.FinalizeResult, error) {
	f.finalize = input
	return &platform.FinalizeResult{Settlement: &platform.SettlementRecord{ID: "settlement-1", EventID: input.EventID, Status: "settled"}}, nil
}

func TestIdempotencyAndReservationRules(t *testing.T) {
	if got := IdempotencyKeyForAction(ActionListing, "version-1"); got != "ecommerce:listing:version-1" {
		t.Fatalf("idempotency key = %s", got)
	}
	if got := IdempotencyKeyForAction("", "job-1"); got != "ecommerce:generation:job-1" {
		t.Fatalf("default action idempotency key = %s", got)
	}
	if got := ReservationKeyForAction(ActionGeneration, "job-1"); got != "reserve:job-1" {
		t.Fatalf("reservation key = %s", got)
	}
}

func TestBeginUsesCompatibleGenerationReservationAndSessionPayload(t *testing.T) {
	fg := &fakeGateway{}
	svc := New(fg)
	ctx, err := svc.Begin(BeginInput{
		Action:           ActionGeneration,
		SourceType:       SourceTypeGenerationImageJob,
		SourceID:         "job-1",
		ProductCode:      DefaultProductCode,
		OrganizationID:   "org-1",
		UserID:           "user-1",
		BillableItemCode: BillableItemImageGenerate,
		ResourceType:     DefaultResourceType,
		UsageUnits:       1,
		IdempotencyKey:   "client-key",
		RouteSnapshot:    map[string]any{"scene_code": "single"},
		Metadata:         map[string]any{"scene_type": "ai_posture"},
	})
	if err != nil {
		t.Fatalf("Begin returned error: %v", err)
	}
	if ctx.ChargeSessionID != "charge-1" || ctx.ReservationID != "reservation-1" {
		t.Fatalf("unexpected context: %+v", ctx)
	}
	if fg.created.ReservationKey != "client-key" {
		t.Fatalf("charge session reservation_key = %s", fg.created.ReservationKey)
	}
	if fg.reserved.ReservationKey != "reserve:job-1" {
		t.Fatalf("resource reservation key = %s", fg.reserved.ReservationKey)
	}
	if fg.created.BillingSubjectType != "organization" || fg.created.BillingSubjectID != "org-1" {
		t.Fatalf("unexpected billing subject: %+v", fg.created)
	}
	var route map[string]any
	if err := json.Unmarshal([]byte(fg.created.RouteSnapshot), &route); err != nil || route["scene_code"] != "single" {
		t.Fatalf("route snapshot = %s err=%v", fg.created.RouteSnapshot, err)
	}
}

func TestCommitAndRelease(t *testing.T) {
	fg := &fakeGateway{}
	svc := New(fg)
	ctx := &Context{
		Action:           ActionGeneration,
		SourceType:       SourceTypeGenerationImageJob,
		SourceID:         "job-1",
		ProductCode:      DefaultProductCode,
		OrganizationID:   "org-1",
		UserID:           "user-1",
		ChargeSessionID:  "charge-1",
		ReservationID:    "reservation-1",
		BillableItemCode: BillableItemImageGenerate,
		ResourceType:     DefaultResourceType,
		UsageUnits:       1,
	}
	commit, err := svc.Commit(CommitInput{Context: ctx, SourceAction: "single", EventID: "evt_job-1", Dimensions: map[string]any{"scene_code": "single"}})
	if err != nil {
		t.Fatalf("Commit returned error: %v", err)
	}
	if commit.EventID != "evt_job-1" || fg.finalize.ReservationID != "reservation-1" || fg.finalize.SourceAction != "single" {
		t.Fatalf("unexpected finalize: commit=%+v input=%+v", commit, fg.finalize)
	}
	if len(fg.updates) != 1 || fg.updates[0].Status != ChargeSessionStatusSettled || fg.updates[0].SettlementID != "settlement-1" {
		t.Fatalf("unexpected settled update: %+v", fg.updates)
	}
	if err := svc.Release(ReleaseInput{Context: ctx, Reason: "runtime_failed"}); err != nil {
		t.Fatalf("Release returned error: %v", err)
	}
	if fg.released != "reservation-1" {
		t.Fatalf("released reservation = %s", fg.released)
	}
	if len(fg.updates) != 2 || fg.updates[1].Status != ChargeSessionStatusReleased {
		t.Fatalf("unexpected release update: %+v", fg.updates)
	}
}
