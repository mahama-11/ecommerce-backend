package workspace

type LocalizedText struct {
	ZH string `json:"zh"`
	EN string `json:"en"`
}

type SavedTemplateRecord struct {
	ID          string       `json:"id"`
	Platform    string       `json:"platform"`
	Tags        []string     `json:"tags"`
	UsageCount  string       `json:"usageCount"`
	Favorite    float64      `json:"favorite"`
	SavedAt     string       `json:"savedAt"`
	SourceType  string       `json:"sourceType,omitempty"`
	SourceLabel string       `json:"sourceLabel,omitempty"`
	ZH          TemplateCopy `json:"zh"`
	EN          TemplateCopy `json:"en"`
}

type TemplateCopy struct {
	Title    string `json:"title"`
	Summary  string `json:"summary"`
	Scenario string `json:"scenario"`
}

type WorkflowEvent struct {
	ID        string        `json:"id"`
	Module    string        `json:"module"`
	Title     LocalizedText `json:"title"`
	Detail    LocalizedText `json:"detail"`
	CreatedAt string        `json:"createdAt"`
}

type LinkedDesignAsset struct {
	ID         string        `json:"id"`
	SourcePath string        `json:"sourcePath"`
	Title      LocalizedText `json:"title"`
	Desc       LocalizedText `json:"desc"`
	SyncedAt   string        `json:"syncedAt"`
}

type LinkedDelivery struct {
	ID         string        `json:"id"`
	SourcePath string        `json:"sourcePath"`
	Title      LocalizedText `json:"title"`
	Size       string        `json:"size"`
	Status     string        `json:"status"`
	Meta       LocalizedText `json:"meta"`
	CreatedAt  string        `json:"createdAt"`
}

type LinkedTemplateBridge struct {
	ID              string        `json:"id"`
	DesignTitle     LocalizedText `json:"designTitle"`
	AITemplateTitle LocalizedText `json:"aiTemplateTitle"`
	Scenario        LocalizedText `json:"scenario"`
	CreatedAt       string        `json:"createdAt"`
}
