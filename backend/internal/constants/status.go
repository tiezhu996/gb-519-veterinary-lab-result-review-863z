package constants

// Shared status values are mirrored in frontend/src/types/status.ts. Keeping
// the lists explicit makes state-machine drift visible during code review.

type SpecimenState string

const (
	SpecimenStateReceived SpecimenState = "received"
	SpecimenStateTesting  SpecimenState = "testing"
	SpecimenStateHold     SpecimenState = "hold"
	SpecimenStateReleased SpecimenState = "released"
	SpecimenStateDisposed SpecimenState = "disposed"
)

var AllSpecimenState = []string{"received", "testing", "hold", "released", "disposed"}

type SignoffState string

const (
	SignoffStateDraft      SignoffState = "draft"
	SignoffStatePeerReview SignoffState = "peer_review"
	SignoffStateSigned     SignoffState = "signed"
	SignoffStateRejected   SignoffState = "rejected"
)

var AllSignoffState = []string{"draft", "peer_review", "signed", "rejected"}

// RiskLevels 为统一的风险等级排序；数字越大风险越高。
var riskRank = map[string]int{"low": 1, "medium": 2, "high": 3, "critical": 4}

// IsHighRiskSignoff 判断签发单是否属于高风险（high/critical）。
func IsHighRiskSignoff(level string) bool {
	return riskRank[level] >= 3
}

// RiskAtLeast 判断 actual 风险是否不低于 required。
func RiskAtLeast(actual, required string) bool {
	return riskRank[actual] >= riskRank[required] && riskRank[actual] > 0
}

var AnimalCaseTransitions = map[string]map[string]bool{
	"registered": {"sampling": true, "testing": true},
	"sampling":   {"testing": true, "closed": true, "registered": true},
	"testing":    {"closed": true, "sampling": true},
	"closed":     {"testing": true},
}

var SpecimenTransitions = map[string]map[string]bool{
	"received": {"testing": true, "hold": true},
	"testing":  {"hold": true, "released": true, "received": true},
	"hold":     {"released": true, "disposed": true, "testing": true},
	"released": {"disposed": true, "hold": true},
	"disposed": {"released": true},
}

var AssayRunTransitions = map[string]map[string]bool{
	"planned":   {"running": true, "validated": true},
	"running":   {"validated": true, "invalid": true, "planned": true},
	"validated": {"invalid": true, "running": true},
	"invalid":   {"validated": true},
}

var ResultSignoffTransitions = map[string]map[string]bool{
	"draft":       {"peer_review": true},
	"peer_review": {"signed": true, "rejected": true},
	"signed":      {},
	"rejected":    {},
}

func CanTransition(graph map[string]map[string]bool, from, to string) bool {
	targets, exists := graph[from]
	return exists && targets[to]
}
