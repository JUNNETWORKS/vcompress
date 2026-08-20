package progress

type Phase string

const (
	PhaseDiscovery  Phase = "discovery"
	PhaseProbe      Phase = "probe"
	PhaseQuality    Phase = "quality"
	PhaseEncode     Phase = "encode"
	PhaseValidation Phase = "validation"
	PhasePublish    Phase = "publish"
)

type Event struct {
	Phase            Phase   `json:"phase"`
	Path             string  `json:"path,omitempty"`
	QualityValue     int     `json:"quality_value,omitempty"`
	Sample           int     `json:"sample,omitempty"`
	SampleCount      int     `json:"sample_count,omitempty"`
	ProcessedSeconds float64 `json:"processed_seconds,omitempty"`
	TotalSeconds     float64 `json:"total_seconds,omitempty"`
	Percent          float64 `json:"percent,omitempty"`
	ETASeconds       float64 `json:"eta_seconds,omitempty"`
	Speed            string  `json:"speed,omitempty"`
	Message          string  `json:"message,omitempty"`
	Total            int     `json:"total,omitempty"`
}

type Reporter interface {
	Report(Event)
}

type ReporterFunc func(Event)

func (f ReporterFunc) Report(event Event) {
	if f != nil {
		f(event)
	}
}

func Emit(reporter Reporter, event Event) {
	if reporter != nil {
		reporter.Report(event)
	}
}
