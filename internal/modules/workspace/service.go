package workspace

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"ecommerce-service/internal/repository"

	"github.com/go-redis/redis/v8"
)

type Service struct {
	repo     *repository.WorkspaceRepository
	redis    *redis.Client
	cacheTTL time.Duration
}

func NewService(repo *repository.WorkspaceRepository, redisClient *redis.Client) *Service {
	return &Service{repo: repo, redis: redisClient, cacheTTL: 5 * time.Minute}
}

func (s *Service) ListSavedTemplates(scope repository.Scope) ([]repository.SavedTemplateRecord, error) {
	return withCache(s, scope, "saved_templates", s.repo.ListSavedTemplates)
}
func (s *Service) SaveTemplate(scope repository.Scope, record repository.SavedTemplateRecord) ([]repository.SavedTemplateRecord, error) {
	items, err := s.repo.SaveTemplate(scope, record)
	if err != nil {
		return nil, err
	}
	s.invalidate(scope, "saved_templates")
	return items, nil
}
func (s *Service) ListWorkflowEvents(scope repository.Scope) ([]repository.WorkflowEventRecord, error) {
	return withCache(s, scope, "workflow_events", s.repo.ListWorkflowEvents)
}
func (s *Service) SaveWorkflowEvent(scope repository.Scope, record repository.WorkflowEventRecord) ([]repository.WorkflowEventRecord, error) {
	items, err := s.repo.SaveWorkflowEvent(scope, record)
	if err != nil {
		return nil, err
	}
	s.invalidate(scope, "workflow_events")
	return items, nil
}
func (s *Service) ListLinkedDesignAssets(scope repository.Scope) ([]repository.LinkedDesignAssetRecord, error) {
	return withCache(s, scope, "linked_design_assets", s.repo.ListLinkedDesignAssets)
}
func (s *Service) SaveLinkedDesignAsset(scope repository.Scope, record repository.LinkedDesignAssetRecord) ([]repository.LinkedDesignAssetRecord, error) {
	items, err := s.repo.SaveLinkedDesignAsset(scope, record)
	if err != nil {
		return nil, err
	}
	s.invalidate(scope, "linked_design_assets")
	return items, nil
}
func (s *Service) ListLinkedDeliveries(scope repository.Scope) ([]repository.LinkedDeliveryRecord, error) {
	return withCache(s, scope, "linked_deliveries", s.repo.ListLinkedDeliveries)
}
func (s *Service) SaveLinkedDelivery(scope repository.Scope, record repository.LinkedDeliveryRecord) ([]repository.LinkedDeliveryRecord, error) {
	items, err := s.repo.SaveLinkedDelivery(scope, record)
	if err != nil {
		return nil, err
	}
	s.invalidate(scope, "linked_deliveries")
	return items, nil
}
func (s *Service) ListTemplateBridges(scope repository.Scope) ([]repository.LinkedTemplateBridgeRecord, error) {
	return withCache(s, scope, "template_bridges", s.repo.ListTemplateBridges)
}
func (s *Service) SaveTemplateBridge(scope repository.Scope, record repository.LinkedTemplateBridgeRecord) ([]repository.LinkedTemplateBridgeRecord, error) {
	items, err := s.repo.SaveTemplateBridge(scope, record)
	if err != nil {
		return nil, err
	}
	s.invalidate(scope, "template_bridges")
	return items, nil
}

func scopeKey(scope repository.Scope, suffix string) string {
	return fmt.Sprintf("ecommerce:%s:%s:%s", suffix, scope.OrgID, scope.UserID)
}

func withCache[T any](s *Service, scope repository.Scope, suffix string, load func(repository.Scope) ([]T, error)) ([]T, error) {
	if s.redis != nil {
		ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
		defer cancel()
		if raw, err := s.redis.Get(ctx, scopeKey(scope, suffix)).Result(); err == nil {
			var items []T
			if jsonErr := json.Unmarshal([]byte(raw), &items); jsonErr == nil {
				return items, nil
			}
		}
	}
	items, err := load(scope)
	if err != nil {
		return nil, err
	}
	if s.redis != nil {
		if payload, jsonErr := json.Marshal(items); jsonErr == nil {
			ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
			defer cancel()
			_ = s.redis.Set(ctx, scopeKey(scope, suffix), payload, s.cacheTTL).Err()
		}
	}
	return items, nil
}

func (s *Service) invalidate(scope repository.Scope, suffix string) {
	if s.redis == nil {
		return
	}
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	_ = s.redis.Del(ctx, scopeKey(scope, suffix)).Err()
}
