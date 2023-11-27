package urth

import (
	"fmt"
	"strconv"
	"time"

	"github.com/sre-norns/urth/pkg/wyrd"

	"gorm.io/datatypes"
	"gorm.io/gorm"
)

// Type to represent an ID of a resource
type ResourceID uint

// ApiToken is opaque datum used for auth purposes
type ApiToken string

type VersionedResourceId struct {
	ID      ResourceID `form:"id" json:"id" yaml:"id" xml:"id"`
	Version uint64     `form:"version" json:"version" yaml:"version" xml:"version"`
}

func NewVersionedId(id uint, version uint64) VersionedResourceId {
	return VersionedResourceId{
		ID:      ResourceID(id),
		Version: version,
	}
}

func (r ResourceID) String() string {
	return strconv.FormatInt(int64(r), 10)
}

func (r VersionedResourceId) String() string {
	return fmt.Sprintf("%v@%d", r.ID, r.Version)
}

type ResourceLabel struct {
	Key   string
	Value string
}

type Resourceable interface {
	GetID() ResourceID
	GetVersionedID() VersionedResourceId
	IsDeleted() bool
}

// ResourceMeta represents common data for all resources managed by the service
type ResourceMeta struct {
	gorm.Model `json:",inline" yaml:",inline"`

	// Unique system generated identified of the resource
	// ID ResourceID `form:"id" json:"id" yaml:"id" xml:"id"`

	// A sequence number representing a specific generation of the resource.
	// Populated by the system. Read-only.
	Version uint64 `form:"version" json:"version" yaml:"version" xml:"version" gorm:"default:1"`

	// Name is a human readable name of the resource used for display in UI
	Name string `form:"name" json:"name" yaml:"name" xml:"name"  binding:"required"`

	Attributes datatypes.JSON `form:"-" json:"-" yaml:"-" xml:"-"`

	// Labels is map of string keys and values that can be used to organize and categorize
	// (scope and select) resources.
	Labels wyrd.Labels `form:"labels,omitempty" json:"labels,omitempty" yaml:"labels,omitempty" xml:"labels,omitempty" gorm:"-"`
}

func (meta *ResourceMeta) GetID() ResourceID {
	return ResourceID(meta.ID)
}

func (meta *ResourceMeta) IsDeleted() bool {
	return meta.DeletedAt.Valid
}

func (meta *ResourceMeta) GetVersionedID() VersionedResourceId {
	return NewVersionedId(meta.ID, meta.Version)
}

// PartialObjectMetadata is a common information about a managed resource without details of that resource.
// TypeMeta represents info about the type of resource.
// This Type is return by API that manage collection of resources.
type PartialObjectMetadata struct {
	TypeMeta `json:",inline" yaml:",inline"`

	// Standard resource's metadata.
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
	IsOnline bool `form:"online" json:"online" yaml:"online" xml:"online" binding:"required"`

	InstanceLabels wyrd.Labels `form:"runner_labels,omitempty" json:"runner_labels,omitempty" yaml:"runner_labels,omitempty" xml:"runner_labels,omitempty" gorm:"-"`
}

type RunnerSpec struct {
	RunnerDefinition   `json:",inline" yaml:",inline"`
	RunnerRegistration `json:",inline" yaml:",inline"`
}

// Runner is a resource manager by Urth service that represents
// an instance of a job processing worker
type Runner struct {
	ResourceMeta `json:",inline" yaml:",inline"`

	RunnerSpec `json:",inline" yaml:",inline"`

	IdToken ApiToken `form:"-" json:"-" yaml:"-" xml:"-"`
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

type JobStatus string
type RunStatus string

const (
	// A new request has been created and is waiting for a runner to pick it up
	JobPending JobStatus = "pending"
	// A runner picked up the job and is currently executing it
	JobRunning JobStatus = "running"
	// No runner picked the job in time and the request expired
	JobExpired JobStatus = "timeout"
	// A runner finished the job and with a status
	JobCompleted JobStatus = "completed"
	// A server failed to schedule the job
	JobErrored JobStatus = "errored"

	// A run completed with a status
	RunFinishedSuccess  RunStatus = "success"
	RunFinishedFailed   RunStatus = "failed"
	RunFinishedError    RunStatus = "errored"
	RunFinishedCanceled RunStatus = "canceled"
	RunFinishedTimeout  RunStatus = "timeout"
)

type ArtifactValue struct {
	// If set, point in time when artifact will expire
	ExpireTime *time.Time `form:"expire_time,omitempty" json:"expire_time,omitempty" yaml:"expire_time,omitempty" xml:"expire_time,omitempty" time_format:"unix" gorm:"type:TIMESTAMP NULL"`

	// Relation type: log / HAR / etc? Determines how content is consumed by clients
	Rel string `form:"rel,omitempty" json:"rel,omitempty" yaml:"rel,omitempty" xml:"rel,omitempty"`

	// MimeType of the content
	MimeType string `form:"mimeType,omitempty" json:"mimeType,omitempty" yaml:"mimeType,omitempty" xml:"mimeType,omitempty"`

	// Blob content of the artifact
	Content []byte `form:"content,omitempty" json:"content,omitempty" yaml:"content,omitempty" xml:"content,omitempty"`
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
	TimeEnded *time.Time `form:"end_time" json:"end_time" yaml:"end_time" xml:"end_time" time_format:"unix" gorm:"type:TIMESTAMP NULL"`

	// Result is a status of the run
	Result RunStatus `form:"result" json:"result" yaml:"result" xml:"result"  binding:"required"`
}

type RunResultOption func(value *FinalRunResults)

func WithTime(value time.Time) RunResultOption {
	return func(result *FinalRunResults) {
		result.TimeEnded = &value
	}
}

func NewRunResults(runResult RunStatus, options ...RunResultOption) FinalRunResults {
	now := time.Now()
	result := FinalRunResults{
		TimeEnded: &now,
		Result:    runResult,
	}

	for _, option := range options {
		option(&result)
	}

	return result
}

// CreateScenarioRunResults is info that runner reports about a running job
type InitialScenarioRunResults struct {
	// Timestamp when a job has been picked-up by a worked
	TimeStarted *time.Time `form:"start_time" json:"start_time" yaml:"start_time" xml:"start_time" gorm:"type:TIMESTAMP NULL"`
}

type ScenarioRunResultSpec struct {
	InitialScenarioRunResults `json:",inline" yaml:",inline"`
	FinalRunResults           `json:",inline" yaml:",inline"`

	Status JobStatus `form:"status" json:"status" yaml:"status" xml:"status"  binding:"required"`
}

// ScenarioRunResults results of a single execution of a given scenario
type ScenarioRunResults struct {
	ResourceMeta `json:",inline" yaml:",inline"`

	ScenarioRunResultSpec `json:",inline" yaml:",inline"`

	UpdateToken ApiToken `uri:"-" form:"-" json:"-" yaml:"-" xml:"-"`
}
