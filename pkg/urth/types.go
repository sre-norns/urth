package urth

import (
	"database/sql"
	"fmt"
	"time"

	"github.com/sre-norns/urth/pkg/wyrd"
	"gorm.io/gorm"
)

// Type to represent an ID of a resource
type ResourceID uint

// ApiToken is opaque datum used for auth purposes
type ApiToken string

type VersionedResourceId struct {
	ID      ResourceID `form:"id" json:"id" yaml:"id" xml:"id"`
	Version uint32     `form:"version" json:"version" yaml:"version" xml:"version"`
}

func NewVersionedId(id uint, version uint32) VersionedResourceId {
	return VersionedResourceId{
		ID:      ResourceID(id),
		Version: version,
	}
}

func (r VersionedResourceId) String() string {
	return fmt.Sprintf("%v@%d", r.ID, r.Version)
}

type ResourceLabel struct {
	Key   string
	Value string
}

type ResourceLabelModel struct {
	OwnerID   int
	OwnerType string

	ResourceLabel
}

// ResourceMeta represents common data for all resources managed by the service
type ResourceMeta struct {
	gorm.Model

	// Unique system generated identified of the resource
	// ID ResourceID `form:"id" json:"id" yaml:"id" xml:"id"`

	// A sequence number representing a specific generation of the resource.
	// Populated by the system. Read-only.
	Version uint32 `form:"version" json:"version" yaml:"version" xml:"version" gorm:"default:1"`

	// Name is a human readable name of the resource used for display in UI
	Name string `form:"name" json:"name" yaml:"name" xml:"name"  binding:"required"`

	// Labels is map of string keys and values that can be used to organize and categorize
	// (scope and select) resources.
	LabelsModel []ResourceLabelModel `form:"-" json:"-" yaml:"-" xml:"-" gorm:"polymorphic:Owner;"`

	Labels wyrd.Labels `form:"labels,omitempty" json:"labels,omitempty" yaml:"labels,omitempty" xml:"labels,omitempty" gorm:"-"`
}

func (meta *ResourceMeta) GetVersionedID() VersionedResourceId {
	return NewVersionedId(meta.ID, meta.Version)
}

// PartialObjectMetadata is a common information about a managed recourse without details of that resource.
// TypeMeta represents info about the type of resource.
// This Type is return by API that manage collection of resources.
type PartialObjectMetadata struct {
	TypeMeta `json:",inline" yaml:",inline"`

	// Standard recourse's metadata.
	ResourceMeta `json:"metadata,omitempty" yaml:"metadata,omitempty"`

	Spec interface{} `json:"spec,omitempty" yaml:"spec,omitempty"`
}

// RunnerDefinition holds information about a runner as supplied by the administrator to register one
type RunnerDefinition struct {
	// Description is a human readable text to describe intent behind this runner
	Description string `form:"description" json:"description,omitempty" yaml:"description,omitempty" xml:"description,omitempty"`

	// Requirements are optional to select sub-set of jobs this worker capable of taking
	Requirements wyrd.LabelSelector `form:"requirements" json:"requirements,omitempty" yaml:"requirements,omitempty" xml:"requirements" gorm:"-"`

	// IsActive is true if this worker is permitted to take on jobs
	IsActive bool `form:"active" json:"active" yaml:"active" xml:"active"`
}

// RunnerRegistration is information that owned and managed by the runner itself
type RunnerRegistration struct {
	// IsOnline is this runner is online and accepts jobs or is currently processing one
	IsOnline bool `form:"online" json:"online" yaml:"online" xml:"online"`

	InstanceLabels wyrd.Labels `form:"runner_labels,omitempty" json:"runner_labels,omitempty" yaml:"runner_labels,omitempty" xml:"runner_labels,omitempty" gorm:"-"`
}

type RunnerSpec struct {
	RunnerDefinition   `json:",inline" yaml:",inline"`
	RunnerRegistration `json:",inline" yaml:",inline"`
}

// Runner is a recourse manager by Urth service that represents
// an instance of a job processing worker
type Runner struct {
	ResourceMeta `json:",inline" yaml:",inline"`

	RunnerSpec `json:",inline" yaml:",inline"`

	IdToken ApiToken `form:"token" json:"token,omitempty" yaml:"token,omitempty" xml:"token,omitempty"`
}

// Type to represent cron-like schedule
type CronSchedule string

type ScenarioScript struct {
	// Kind identifies the type of content this scenario implementing
	Kind ScenarioKind `form:"kind" json:"kind,omitempty" yaml:"kind,omitempty" xml:"kind"`

	// Timeout
	Timeout time.Duration `form:"timeout" json:"timeout,omitempty" yaml:"timeout,omitempty" xml:"timeout,omitempty"`

	// Actual script, of a 'kind' type
	Content []byte `form:"content" json:"content,omitempty" yaml:"content,omitempty" xml:"content"`
}

type CreateScenario struct {
	// Description is a human readable text to describe the scenario
	Description string `form:"description" json:"description,omitempty" yaml:"description,omitempty" xml:"description"`

	// Requirements are optional to select sub-set of runners that are qualified to perform the script.
	Requirements wyrd.LabelSelector `form:"requirements" json:"requirements,omitempty" yaml:"requirements,omitempty" xml:"requirements" gorm:"-"`

	// A schedule to run the script
	RunSchedule CronSchedule `form:"schedule" json:"schedule,omitempty" yaml:"schedule,omitempty" xml:"schedule"`

	// IsActive - scenario state: If false scenario will not be picked up for scheduling
	IsActive bool `form:"active" json:"active" yaml:"active" xml:"active"`

	// Script is the actual test scenario that a qualified runner executes
	Script *ScenarioScript `form:"script" json:"script,omitempty" yaml:"script,omitempty" xml:"script" gorm:"embedded;embeddedPrefix:script_"`
}

type Scenario struct {
	ResourceMeta `json:",inline" yaml:",inline"`

	CreateScenario `json:",inline" yaml:",inline"`
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
	// Id of ScenarioRun that produced this artifact
	// ScenarioRunResultsID uint `form:"scenarioRunId" json:"scenarioRunId" yaml:"scenarioRunId" xml:"scenarioRunId"`

	// If set, point in time when artifact will expire
	ExpireTime sql.NullTime `form:"expire_time" json:"expire_time" yaml:"expire_time" xml:"expire_time" time_format:"unix"`

	// Relation type: log / HAR / etc? Determines how content is consumed by clients
	Rel string `form:"rel" json:"rel" yaml:"rel" xml:"rel"`

	// MimeType of the content
	MimeType string `form:"mimeType" json:"mimeType" yaml:"mimeType" xml:"mimeType"`

	// Blob content of the artifact
	Content []byte `form:"content" json:"content" yaml:"content" xml:"content"`
}

// Artifact model. Artifacts are produced and published by a script runner,
// as a result of scenario execution.
type Artifact struct {
	ResourceMeta `json:",inline" yaml:",inline"`

	ArtifactValue `json:",inline" yaml:",inline"`
}

// Final results of the script run
type FinalRunResults struct {
	// Timestamp when execution finished, if it finished
	TimeEnded sql.NullTime `form:"end_time" json:"end_time" yaml:"end_time" xml:"end_time" time_format:"unix"  binding:"required"`

	// Result is a status of the run
	Result RunStatus `form:"result" json:"result" yaml:"result" xml:"result"  binding:"required"`

	// TODO:
	// Artifacts []Artifact `json:"-" yaml:"-" gorm:"polymorphic:Owner;"`
	// Artifacts []Artifact `json:"artifacts,omitempty" yaml:"artifacts,omitempty" gorm:"foreignKey:ScenarioRunResultsID"`
	// ArtifactIds []Artifact `json:"artifactIds,omitempty" yaml:"artifactIds,omitempty"`
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

// func WithArtifacts(artifacts ...ArtifactValue) RunResultOption {
// 	return func(result *FinalRunResults) {
// 		for _, artifact := range artifacts {
// 			result.Artifacts = append(result.Artifacts, Artifact{
// 				ArtifactValue: artifact,
// 			})
// 		}
// 	}
// }

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

// CreateScenarioRunResults is info that runner reports about a running job
type InitialScenarioRunResults struct {
	// ID and version of the scenario that this results were produced for
	ScenarioID VersionedResourceId `form:"play_id" json:"play_id" yaml:"play_id" xml:"play_id"  binding:"required"  gorm:"embedded;embeddedPrefix:scenario_"`
	// ID and version of the runner that executed the scenario
	RunnerID VersionedResourceId `form:"runner_id" json:"runner_id" yaml:"runner_id" xml:"runner_id"  binding:"required"  gorm:"embedded;embeddedPrefix:runner_"`
	// Timestamp when execution started
	TimeStarted time.Time `form:"start_time" json:"start_time" yaml:"start_time" xml:"start_time" binding:"required"`
}

type ScenarioRunResultSpec struct {
	InitialScenarioRunResults `json:",inline" yaml:",inline"`
	FinalRunResults           `json:",inline" yaml:",inline"`
}

// ScenarioRunResults results of a single execution of a given scenario
type ScenarioRunResults struct {
	ResourceMeta `json:",inline" yaml:",inline"`

	ScenarioRunResultSpec `json:",inline" yaml:",inline"`

	UpdateToken ApiToken `uri:"-" form:"-" json:"-" yaml:"-" xml:"-"`
}
