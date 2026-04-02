package hiring

type BlockSeverity string

const (
	SeverityInfo  BlockSeverity = "info"
	SeverityWarn  BlockSeverity = "warn"
	SeverityBlock BlockSeverity = "block"
)

type TransitionInput struct {
	ApplicationID string
	FromStage     string
	ToStage       string
	Provided      map[string]string
}
