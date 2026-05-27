package billinggate

import (
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"ecommerce-service/internal/observability"
	"ecommerce-service/internal/platform"
)

const (
	// ActionGeneration gates image/content generation workloads.
	ActionGeneration = "generation"
	// ActionListing gates listing draft/version creation workloads.
	ActionListing = "listing"
	// ActionExport gates export/package/download workloads.
	ActionExport = "export"

	DefaultProductCode           = "ecommerce"
	DefaultBillingSubjectType    = "organization"
	DefaultUnit                  = "action"
	DefaultResourceType          = "quota"
	BillableItemImageGenerate    = "ecommerce.image.generate"
	BillableItemListingGenerate  = "ecommerce.listing.generate"
	BillableItemExportGenerate   = "ecommerce.export.generate"
	SourceTypeGenerationImageJob = "ecommerce_image_job"
	SourceTypeListingVersion     = "ecommerce_listing_version"
	SourceTypeExportTask         = "ecommerce_export_task"
	ChargeSessionStatusReserved  = "reserved"
	ChargeSessionStatusSettled   = "settled"
	ChargeSessionStatusReleased  = "released"
)

// PlatformGateway is the subset of the platform internal billing/usage API used
// by the product-side gate. *platform.Client satisfies this interface.
type PlatformGateway interface {
	CreateChargeSession(platform.CreateChargeSessionInput) (*platform.ChargeSession, error)
	UpdateChargeSession(string, platform.UpdateChargeSessionInput) (*platform.ChargeSession, error)
	ReserveResources(platform.ReserveInput) (*platform.ResourceReservation, error)
	ReleaseReservation(string) (*platform.ResourceReservation, error)
	FinalizeMetering(platform.FinalizeInput) (*platform.FinalizeResult, error)
}

type Service struct {
	platform PlatformGateway
}

type Context struct {
	Action           string
	SourceType       string
	SourceID         string
	ProductCode      string
	OrganizationID   string
	UserID           string
	ChargeSessionID  string
	ReservationID    string
	BillableItemCode string
	ResourceType     string
	ReservationKey   string
	UsageUnits       int64
}

type BeginInput struct {
	Action           string
	SourceType       string
	SourceID         string
	ProductCode      string
	OrganizationID   string
	UserID           string
	BillableItemCode string
	ResourceType     string
	UsageUnits       int64
	IdempotencyKey   string
	RouteSnapshot    map[string]any
	Metadata         map[string]any
}

type CommitInput struct {
	Context      *Context
	SourceAction string
	EventID      string
	OccurredAt   time.Time
	Dimensions   map[string]any
	Metadata     map[string]any
}

type CommitResult struct {
	EventID string
	Result  *platform.FinalizeResult
}

type ReleaseInput struct {
	Context  *Context
	Reason   string
	Metadata map[string]any
}

func New(platformGateway PlatformGateway) *Service {
	return &Service{platform: platformGateway}
}

// IdempotencyKeyForAction is the canonical product-side key rule for future gate
// callers. Existing callers may still pass a user-provided key; if they do not,
// use this deterministic key per action and business subject.
func IdempotencyKeyForAction(action, subjectID string) string {
	action = NormalizeAction(action)
	subjectID = strings.TrimSpace(subjectID)
	if subjectID == "" {
		return ""
	}
	return fmt.Sprintf("%s:%s:%s", DefaultProductCode, action, subjectID)
}

// ReservationKeyForAction is the shared reservation-key rule. It preserves the
// current generation convention (reserve:<jobID>) while allowing action-aware
// idempotency through charge-session ReservationKey / runtime IdempotencyKey.
func ReservationKeyForAction(_ string, subjectID string) string {
	subjectID = strings.TrimSpace(subjectID)
	if subjectID == "" {
		return ""
	}
	return fmt.Sprintf("reserve:%s", subjectID)
}

func NormalizeAction(action string) string {
	switch strings.ToLower(strings.TrimSpace(action)) {
	case ActionListing:
		return ActionListing
	case ActionExport:
		return ActionExport
	default:
		return ActionGeneration
	}
}

func (s *Service) Begin(input BeginInput) (*Context, error) {
	startedAt := time.Now()
	observability.Event("ecommerce.runtime.charge.reserve.started", "runtime_billing", "charge.reserve", observability.Fields{"product_id": input.ProductCode, "job_id": input.SourceID, "operation": input.Action, "billable_item_code": input.BillableItemCode})
	if s == nil || s.platform == nil {
		return nil, fmt.Errorf("billing gate platform client is required")
	}
	input.Action = NormalizeAction(input.Action)
	input.ProductCode = firstNonEmpty(input.ProductCode, DefaultProductCode)
	input.ResourceType = firstNonEmpty(input.ResourceType, DefaultResourceType)
	input.UsageUnits = positiveOrDefault(input.UsageUnits, 1)
	reservationKey := ReservationKeyForAction(input.Action, input.SourceID)
	chargeReservationKey := firstNonEmpty(strings.TrimSpace(input.IdempotencyKey), reservationKey)
	observability.Event("ecommerce.runtime.charge_session.create.started", "runtime_billing", "charge_session.create", observability.Fields{"product_id": input.ProductCode, "job_id": input.SourceID, "operation": input.Action})
	session, err := s.platform.CreateChargeSession(platform.CreateChargeSessionInput{
		SourceType:         input.SourceType,
		SourceID:           input.SourceID,
		ProductCode:        input.ProductCode,
		OrganizationID:     input.OrganizationID,
		UserID:             input.UserID,
		BillingSubjectType: DefaultBillingSubjectType,
		BillingSubjectID:   input.OrganizationID,
		BillableItemCode:   input.BillableItemCode,
		ResourceType:       input.ResourceType,
		ReservationKey:     chargeReservationKey,
		EstimatedUnits:     input.UsageUnits,
		RouteSnapshot:      mustMarshal(input.RouteSnapshot),
		Metadata:           mustMarshal(input.Metadata),
	})
	if err != nil {
		observability.ErrorEvent("ecommerce.runtime.charge_session.create.failed", "runtime_billing", "charge_session.create", err, "charge_session_create_failed", observability.Fields{"product_id": input.ProductCode, "job_id": input.SourceID})
		observability.ErrorEvent("ecommerce.runtime.charge.reserve.failed", "runtime_billing", "charge.reserve", err, "charge_session_create_failed", observability.Fields{"product_id": input.ProductCode, "job_id": input.SourceID, "latency_ms": time.Since(startedAt).Milliseconds()})
		return nil, err
	}
	observability.Event("ecommerce.runtime.charge_session.create.finished", "runtime_billing", "charge_session.create", observability.Fields{"product_id": input.ProductCode, "job_id": input.SourceID, "session_id": session.ID})
	observability.Event("ecommerce.runtime.reservation.create.started", "runtime_billing", "reservation.create", observability.Fields{"product_id": input.ProductCode, "job_id": input.SourceID, "session_id": session.ID})
	reservation, err := s.platform.ReserveResources(platform.ReserveInput{
		ResourceType:       input.ResourceType,
		BillingSubjectType: DefaultBillingSubjectType,
		BillingSubjectID:   input.OrganizationID,
		BillableItemCode:   input.BillableItemCode,
		ReservationKey:     reservationKey,
		ReferenceID:        session.ID,
		Units:              input.UsageUnits,
		Metadata:           mustMarshal(map[string]any{"source_id": input.SourceID, "charge_session_id": session.ID, "action": input.Action}),
	})
	if err != nil {
		observability.ErrorEvent("ecommerce.runtime.reservation.create.failed", "runtime_billing", "reservation.create", err, "reservation_create_failed", observability.Fields{"product_id": input.ProductCode, "job_id": input.SourceID, "session_id": session.ID})
		observability.ErrorEvent("ecommerce.runtime.charge.reserve.failed", "runtime_billing", "charge.reserve", err, "reservation_create_failed", observability.Fields{"product_id": input.ProductCode, "job_id": input.SourceID, "session_id": session.ID, "latency_ms": time.Since(startedAt).Milliseconds()})
		return nil, err
	}
	if reservation == nil || strings.TrimSpace(reservation.ID) == "" {
		err := fmt.Errorf("resource reservation missing for %s %s", input.Action, input.SourceID)
		observability.ErrorEvent("ecommerce.runtime.charge.reserve.failed", "runtime_billing", "charge.reserve", err, "reservation_missing", observability.Fields{"product_id": input.ProductCode, "job_id": input.SourceID, "session_id": session.ID, "latency_ms": time.Since(startedAt).Milliseconds()})
		return nil, err
	}
	observability.Event("ecommerce.runtime.reservation.create.finished", "runtime_billing", "reservation.create", observability.Fields{"product_id": input.ProductCode, "job_id": input.SourceID, "session_id": session.ID, "reservation_id": reservation.ID})
	observability.Event("ecommerce.runtime.charge.reserve.finished", "runtime_billing", "charge.reserve", observability.Fields{"product_id": input.ProductCode, "job_id": input.SourceID, "session_id": session.ID, "reservation_id": reservation.ID, "latency_ms": time.Since(startedAt).Milliseconds()})
	return &Context{
		Action:           input.Action,
		SourceType:       input.SourceType,
		SourceID:         input.SourceID,
		ProductCode:      input.ProductCode,
		OrganizationID:   input.OrganizationID,
		UserID:           input.UserID,
		ChargeSessionID:  session.ID,
		ReservationID:    reservation.ID,
		BillableItemCode: input.BillableItemCode,
		ResourceType:     input.ResourceType,
		ReservationKey:   reservationKey,
		UsageUnits:       input.UsageUnits,
	}, nil
}

func (s *Service) MarkReserved(ctx *Context, metadata map[string]any) error {
	if s == nil || s.platform == nil {
		return fmt.Errorf("billing gate platform client is required")
	}
	if ctx == nil || strings.TrimSpace(ctx.ChargeSessionID) == "" {
		return fmt.Errorf("billing gate charge session is required")
	}
	observability.Event("ecommerce.runtime.charge.mark_reserved.started", "runtime_billing", "charge.mark_reserved", observability.Fields{"job_id": ctx.SourceID, "session_id": ctx.ChargeSessionID, "reservation_id": ctx.ReservationID})
	_, err := s.platform.UpdateChargeSession(ctx.ChargeSessionID, platform.UpdateChargeSessionInput{
		Status:        ChargeSessionStatusReserved,
		ReservationID: ctx.ReservationID,
		Metadata:      mustMarshal(metadata),
	})
	if err != nil {
		observability.ErrorEvent("ecommerce.runtime.charge.mark_reserved.failed", "runtime_billing", "charge.mark_reserved", err, "charge_mark_reserved_failed", observability.Fields{"job_id": ctx.SourceID, "session_id": ctx.ChargeSessionID})
		return err
	}
	observability.Event("ecommerce.runtime.charge.mark_reserved.finished", "runtime_billing", "charge.mark_reserved", observability.Fields{"job_id": ctx.SourceID, "session_id": ctx.ChargeSessionID, "reservation_id": ctx.ReservationID})
	return nil
}

func (s *Service) Commit(input CommitInput) (*CommitResult, error) {
	startedAt := time.Now()
	ctx := input.Context
	if s == nil || s.platform == nil {
		return nil, fmt.Errorf("billing gate platform client is required")
	}
	if ctx == nil || strings.TrimSpace(ctx.ChargeSessionID) == "" {
		return nil, fmt.Errorf("billing gate charge session is required")
	}
	eventID := firstNonEmpty(input.EventID, fmt.Sprintf("evt_%s", ctx.SourceID))
	occurredAt := input.OccurredAt
	if occurredAt.IsZero() {
		occurredAt = time.Now().UTC()
	}
	observability.Event("ecommerce.runtime.settlement.finalize.started", "runtime_billing", "settlement.finalize", observability.Fields{"job_id": ctx.SourceID, "session_id": ctx.ChargeSessionID, "reservation_id": ctx.ReservationID})
	result, err := s.platform.FinalizeMetering(platform.FinalizeInput{
		FinalizationID: fmt.Sprintf("fin_%s", ctx.SourceID),
		ReservationID:  ctx.ReservationID,
		IngestEventInput: platform.IngestEventInput{
			EventID:            eventID,
			SourceType:         ctx.SourceType,
			SourceID:           ctx.SourceID,
			SourceAction:       input.SourceAction,
			ProductCode:        firstNonEmpty(ctx.ProductCode, DefaultProductCode),
			OrgID:              ctx.OrganizationID,
			UserID:             ctx.UserID,
			BillableItemCode:   ctx.BillableItemCode,
			ChargeGroupID:      ctx.SourceID,
			BillingSubjectType: DefaultBillingSubjectType,
			BillingSubjectID:   ctx.OrganizationID,
			UsageUnits:         positiveOrDefault(ctx.UsageUnits, 1),
			Unit:               DefaultUnit,
			OccurredAt:         occurredAt.Format(time.RFC3339),
			Dimensions:         mustMarshal(input.Dimensions),
			Metadata:           mustMarshal(input.Metadata),
		},
	})
	if err != nil {
		observability.ErrorEvent("ecommerce.runtime.settlement.finalize.failed", "runtime_billing", "settlement.finalize", err, "finalize_metering_failed", observability.Fields{"job_id": ctx.SourceID, "session_id": ctx.ChargeSessionID, "latency_ms": time.Since(startedAt).Milliseconds()})
		return nil, err
	}
	finalUnits := positiveOrDefault(ctx.UsageUnits, 1)
	settlementID := ""
	if result != nil && result.Settlement != nil {
		settlementID = result.Settlement.ID
	}
	if _, err := s.platform.UpdateChargeSession(ctx.ChargeSessionID, platform.UpdateChargeSessionInput{
		Status:        ChargeSessionStatusSettled,
		ReservationID: ctx.ReservationID,
		EventID:       eventID,
		SettlementID:  settlementID,
		FinalUnits:    &finalUnits,
		Metadata:      mustMarshal(map[string]any{"event_id": eventID}),
	}); err != nil {
		observability.ErrorEvent("ecommerce.runtime.settlement.finalize.failed", "runtime_billing", "settlement.finalize", err, "charge_session_settle_failed", observability.Fields{"job_id": ctx.SourceID, "session_id": ctx.ChargeSessionID, "event_id": eventID, "latency_ms": time.Since(startedAt).Milliseconds()})
		return nil, err
	}
	observability.Event("ecommerce.runtime.settlement.finalize.finished", "runtime_billing", "settlement.finalize", observability.Fields{"job_id": ctx.SourceID, "session_id": ctx.ChargeSessionID, "event_id": eventID, "settlement_id": settlementID, "latency_ms": time.Since(startedAt).Milliseconds()})
	return &CommitResult{EventID: eventID, Result: result}, nil
}

func (s *Service) Release(input ReleaseInput) error {
	ctx := input.Context
	if s == nil || s.platform == nil || ctx == nil {
		return nil
	}
	if strings.TrimSpace(ctx.ReservationID) != "" {
		_, _ = s.platform.ReleaseReservation(ctx.ReservationID)
	}
	if strings.TrimSpace(ctx.ChargeSessionID) != "" {
		metadata := input.Metadata
		if metadata == nil {
			metadata = map[string]any{}
		}
		if strings.TrimSpace(input.Reason) != "" {
			metadata["release_reason"] = input.Reason
		}
		_, _ = s.platform.UpdateChargeSession(ctx.ChargeSessionID, platform.UpdateChargeSessionInput{
			Status:        ChargeSessionStatusReleased,
			ReservationID: ctx.ReservationID,
			Metadata:      mustMarshal(metadata),
		})
	}
	return nil
}

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		if strings.TrimSpace(value) != "" {
			return strings.TrimSpace(value)
		}
	}
	return ""
}

func positiveOrDefault(value, fallback int64) int64 {
	if value > 0 {
		return value
	}
	return fallback
}

func mustMarshal(value map[string]any) string {
	if len(value) == 0 {
		return "{}"
	}
	data, err := json.Marshal(value)
	if err != nil {
		return "{}"
	}
	return string(data)
}
