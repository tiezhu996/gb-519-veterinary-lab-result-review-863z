package model

import "time"

// ResultSignoff models 结果签发 as an independently versioned aggregate. The fields
// cover ownership, operational context, evidence and measured risk so later
// changes naturally span persistence, service and UI layers.
type ResultSignoff struct {
	BaseModel
	Facility     string    `json:"facility" gorm:"size:120;index"`
	Owner        string    `json:"owner" gorm:"size:120;index"`
	Category     string    `json:"category" gorm:"size:80;index"`
	RiskLevel    string    `json:"riskLevel" gorm:"size:32;index"`
	MetricValue  float64   `json:"metricValue"`
	MetricUnit   string    `json:"metricUnit" gorm:"size:24"`
	EffectiveAt  time.Time `json:"effectiveAt"`
	Evidence     string    `json:"evidence" gorm:"size:2000"`
	RelatedCode  string    `json:"relatedCode" gorm:"size:64;index"`
	PreparedBy   string    `json:"preparedBy" gorm:"size:80;index"`
	ReviewedBy   string    `json:"reviewedBy" gorm:"size:80;index"`
	ReviewReason string    `json:"reviewReason" gorm:"size:500"`
	// ReviewBasis 是复核员在签发高风险结果时填写的复核依据，不接受口头说明。
	ReviewBasis string `json:"reviewBasis" gorm:"size:1000"`
	// AssayEvidence 是本次签发版本关联检测运行的证据快照（运行编号、状态、检测指标、证据）。
	AssayEvidence *SignoffAssayEvidence `json:"assayEvidence,omitempty" gorm:"foreignKey:ResultSignoffID;references:ID"`
	// PendingBlockReason 记录高风险签发被服务端挡下时缺失的条件；记录留在待复核。
	PendingBlockReason string                  `json:"pendingBlockReason,omitempty" gorm:"size:1000"`
	Revisions          []ResultSignoffRevision `json:"revisions,omitempty" gorm:"foreignKey:ResultSignoffID"`
}

func (item *ResultSignoff) GetBase() *BaseModel { return &item.BaseModel }

func (item ResultSignoff) TableName() string { return "result_signoffs" }

var ResultSignoffInitialStatus = "draft"

// ResultSignoffRevision is append-only evidence for every signoff version.
type ResultSignoffRevision struct {
	ID              uint   `json:"id" gorm:"primaryKey"`
	ResultSignoffID uint   `json:"resultSignoffId" gorm:"not null;index;uniqueIndex:idx_signoff_revision_version,priority:1"`
	Version         uint   `json:"version" gorm:"not null;uniqueIndex:idx_signoff_revision_version,priority:2"`
	Status          string `json:"status" gorm:"size:40;not null"`
	Evidence        string `json:"evidence" gorm:"size:2000"`
	Actor           string `json:"actor" gorm:"size:80;not null;index"`
	RequestID       string `json:"requestId" gorm:"size:64;not null;index"`
	Action          string `json:"action" gorm:"size:40;not null"`
	Reason          string `json:"reason" gorm:"size:500"`
	// ReviewBasis 是高风险签发时复核员写入的复核依据；低风险版本为空。
	ReviewBasis string `json:"reviewBasis,omitempty" gorm:"size:1000"`
	// 以下字段为签发版本关联检测运行的证据快照，历史版本可直接看到当时依据。
	AssayCode       string    `json:"assayCode,omitempty" gorm:"size:64;index"`
	AssayStatus     string    `json:"assayStatus,omitempty" gorm:"size:40"`
	AssayMetricName string    `json:"assayMetricName,omitempty" gorm:"size:80"`
	AssayMetricVal  float64   `json:"assayMetricValue,omitempty"`
	AssayMetricUnit string    `json:"assayMetricUnit,omitempty" gorm:"size:24"`
	AssayEvidence   string    `json:"assayEvidence,omitempty" gorm:"size:2000"`
	CreatedAt       time.Time `json:"createdAt" gorm:"index"`
}

// SignoffAssayEvidence 是关联检测运行在本次复核版本上的证据快照（一对一）。
type SignoffAssayEvidence struct {
	ID              uint      `json:"id" gorm:"primaryKey"`
	ResultSignoffID uint      `json:"resultSignoffId" gorm:"not null;uniqueIndex"`
	AssayCode       string    `json:"assayCode" gorm:"size:64;index"`
	AssayStatus     string    `json:"assayStatus" gorm:"size:40"`
	AssayMetricName string    `json:"assayMetricName" gorm:"size:80"`
	AssayMetricVal  float64   `json:"assayMetricValue"`
	AssayMetricUnit string    `json:"assayMetricUnit" gorm:"size:24"`
	AssayEvidence   string    `json:"assayEvidence" gorm:"size:2000"`
	CreatedAt       time.Time `json:"createdAt"`
	UpdatedAt       time.Time `json:"updatedAt"`
}
