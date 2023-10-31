package urth

import (
	"database/sql"
	"time"
)

// Type to represent an ID of a resource
type ResourceID uint

type ScenarioScript struct {
	// Kind identifies the type of content this scenario implementing
	Kind ScenarioKind `form:"kind" json:"kind" yaml:"kind" xml:"kind"  binding:"required"`

	// Actual script, of a 'kind' type
	Content []byte `form:"content" json:"content" yaml:"content" xml:"content"  binding:"required"`
}

type RunStatus string

const (
	RunStatusPending    RunStatus = "pending"
	RunFinishedSuccess  RunStatus = "success"
	RunFinishedFailed   RunStatus = "failed"
	RunFinishedError    RunStatus = "errored"
	RunFinishedCanceled RunStatus = "canceled"
	RunFinishedTimeout  RunStatus = "timeout"
)

type ArtifactValue struct {
	Rel      string `form:"rel" json:"rel" yaml:"rel" xml:"rel"`
	MimeType string `form:"mimeType" json:"mimeType" yaml:"mimeType" xml:"mimeType"`
	Content  []byte `form:"content" json:"content" yaml:"content" xml:"content"`
}

// Artifact model as produced by the script runner
type Artifact struct {
	OwnerID   int
	OwnerType string

	ArtifactValue `json:",inline" yaml:",inline"`
}

// Final results of the script run
type FinalRunResults struct {
	// Timestamp when execution finished, if it finished
	TimeEnded sql.NullTime `form:"end_time" json:"end_time" yaml:"end_time" xml:"end_time" time_format:"unix"  binding:"required"`

	// Result is a status of the run
	Result RunStatus `form:"result" json:"result" yaml:"result" xml:"result"  binding:"required"`

	// TODO:
	Artifacts []Artifact `gorm:"polymorphic:Owner;"`
}

type RunResultOption func(value *FinalRunResults)

func WithTime(value time.Time) RunResultOption {
	return func(result *FinalRunResults) {
		result.TimeEnded = sql.NullTime{
			Time:  value,
			Valid: true,
		}
	}
}

func WithArtifacts(artifacts ...ArtifactValue) RunResultOption {
	return func(result *FinalRunResults) {
		for _, artifact := range artifacts {
			result.Artifacts = append(result.Artifacts, Artifact{
				ArtifactValue: artifact,
			})
		}
	}
}

func NewRunResults(runResult RunStatus, options ...RunResultOption) FinalRunResults {
	result := FinalRunResults{
		TimeEnded: sql.NullTime{
			Time:  time.Now(),
			Valid: true,
		},
		Result: runResult,
	}

	for _, option := range options {
		option(&result)
	}

	return result
}
