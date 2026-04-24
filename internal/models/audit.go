package models

import "time"

type AuditLog struct {
	ID             string    `gorm:"primaryKey;type:varchar(64)" json:"id"`
	RequestID      string    `gorm:"index" json:"request_id"`
	TraceID        string    `gorm:"index" json:"trace_id"`
	ActorUserID    string    `gorm:"index" json:"actor_user_id"`
	ActorOrgID     string    `gorm:"index" json:"actor_org_id"`
	Action         string    `gorm:"index;not null" json:"action"`
	TargetType     string    `gorm:"index;not null" json:"target_type"`
	TargetID       string    `gorm:"index" json:"target_id"`
	Status         string    `gorm:"index;not null" json:"status"`
	Route          string    `json:"route"`
	Method         string    `json:"method"`
	Details        string    `gorm:"type:text" json:"details"`
	BeforeSnapshot string    `gorm:"type:text" json:"before_snapshot"`
	AfterSnapshot  string    `gorm:"type:text" json:"after_snapshot"`
	DiffSummary    string    `gorm:"type:text" json:"diff_summary"`
	CreatedAt      time.Time `json:"created_at"`
}
