package audit

import (
	"encoding/json"
	"reflect"
	"sort"
	"time"

	"ecommerce-service/internal/models"
	"ecommerce-service/internal/repository"

	"github.com/gin-gonic/gin"
)

type Service struct {
	repo *repository.AuditRepository
}

type HistoryItem struct {
	ID          string         `json:"id"`
	RequestID   string         `json:"request_id,omitempty"`
	TraceID     string         `json:"trace_id,omitempty"`
	Action      string         `json:"action"`
	TargetType  string         `json:"target_type"`
	TargetID    string         `json:"target_id,omitempty"`
	Status      string         `json:"status"`
	Route       string         `json:"route,omitempty"`
	Method      string         `json:"method,omitempty"`
	Details     string         `json:"details,omitempty"`
	DiffSummary string         `json:"diff_summary,omitempty"`
	CreatedAt   string         `json:"created_at"`
	Before      map[string]any `json:"before,omitempty"`
	After       map[string]any `json:"after,omitempty"`
}

type HistoryResult struct {
	Items []HistoryItem `json:"items"`
	Total int64         `json:"total"`
}

type RecordInput struct {
	Action         string
	TargetType     string
	TargetID       string
	Status         string
	Details        string
	BeforeSnapshot any
	AfterSnapshot  any
}

func NewService(repo *repository.AuditRepository) *Service {
	return &Service{repo: repo}
}

func (s *Service) RecordFromGin(c *gin.Context, input RecordInput) error {
	beforeSnapshot, _ := encodeSnapshot(input.BeforeSnapshot)
	afterSnapshot, _ := encodeSnapshot(input.AfterSnapshot)
	item := &models.AuditLog{
		ID:             buildID("audit"),
		RequestID:      c.GetString("requestID"),
		TraceID:        c.GetString("traceID"),
		ActorUserID:    c.GetString("userID"),
		ActorOrgID:     c.GetString("orgID"),
		Action:         input.Action,
		TargetType:     input.TargetType,
		TargetID:       input.TargetID,
		Status:         defaultString(input.Status, "success"),
		Route:          c.FullPath(),
		Method:         c.Request.Method,
		Details:        input.Details,
		BeforeSnapshot: beforeSnapshot,
		AfterSnapshot:  afterSnapshot,
		DiffSummary:    buildDiffSummary(input.BeforeSnapshot, input.AfterSnapshot),
		CreatedAt:      time.Now(),
	}
	return s.repo.Create(item)
}

func (s *Service) History(actorOrgID, actorUserID, targetType, status string, limit, offset int) (*HistoryResult, error) {
	items, total, err := s.repo.List(actorOrgID, actorUserID, targetType, status, limit, offset)
	if err != nil {
		return nil, err
	}
	out := make([]HistoryItem, 0, len(items))
	for _, item := range items {
		out = append(out, HistoryItem{ID: item.ID, RequestID: item.RequestID, TraceID: item.TraceID, Action: item.Action, TargetType: item.TargetType, TargetID: item.TargetID, Status: item.Status, Route: item.Route, Method: item.Method, Details: item.Details, DiffSummary: item.DiffSummary, CreatedAt: item.CreatedAt.UTC().Format(time.RFC3339), Before: decodeSnapshot(item.BeforeSnapshot), After: decodeSnapshot(item.AfterSnapshot)})
	}
	return &HistoryResult{Items: out, Total: total}, nil
}

func encodeSnapshot(value any) (string, error) {
	if value == nil {
		return "", nil
	}
	data, err := json.Marshal(value)
	if err != nil {
		return "", err
	}
	return string(data), nil
}

func decodeSnapshot(raw string) map[string]any {
	if raw == "" {
		return map[string]any{}
	}
	out := map[string]any{}
	if err := json.Unmarshal([]byte(raw), &out); err != nil {
		return map[string]any{}
	}
	return out
}

func buildDiffSummary(before, after any) string {
	if before == nil && after == nil {
		return ""
	}
	if before == nil {
		keys := snapshotKeys(after)
		return "created:" + joinKeys(keys)
	}
	if after == nil {
		keys := snapshotKeys(before)
		return "deleted:" + joinKeys(keys)
	}
	beforeMap := toMap(before)
	afterMap := toMap(after)
	keySet := map[string]struct{}{}
	for key := range beforeMap {
		keySet[key] = struct{}{}
	}
	for key := range afterMap {
		keySet[key] = struct{}{}
	}
	changed := make([]string, 0, len(keySet))
	for key := range keySet {
		if !reflect.DeepEqual(beforeMap[key], afterMap[key]) {
			changed = append(changed, key)
		}
	}
	sort.Strings(changed)
	return "changed:" + joinKeys(changed)
}

func snapshotKeys(value any) []string {
	keys := make([]string, 0)
	for key := range toMap(value) {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	return keys
}

func toMap(value any) map[string]any {
	if value == nil {
		return map[string]any{}
	}
	data, err := json.Marshal(value)
	if err != nil {
		return map[string]any{}
	}
	out := map[string]any{}
	if err := json.Unmarshal(data, &out); err != nil {
		return map[string]any{}
	}
	return out
}

func joinKeys(keys []string) string {
	if len(keys) == 0 {
		return ""
	}
	data, _ := json.Marshal(keys)
	return string(data)
}

func defaultString(value, fallback string) string {
	if value == "" {
		return fallback
	}
	return value
}

func buildID(prefix string) string {
	return prefix + "_" + time.Now().Format("20060102150405.000000000")
}
