package urth

import (
	"fmt"
	"time"

	"github.com/adhocore/gronx"
	"github.com/sre-norns/wyrd/pkg/manifest"
	"gorm.io/gorm"
)

var (
	ErrResourceNameEmpty = fmt.Errorf("manifest %w", manifest.ErrNameIsEmpty)
)

// WorkerInstanceSpec defines details about an instance of worker active
type WorkerInstanceSpec struct {
	RunnerID manifest.ResourceID `json:"-" yaml:"-"`
	// Runner is the 'class' that this worker is an instance of
	Runner Runner `json:"-" yaml:"-" gorm:"foreignKey:RunnerID;references:UID"`

	// IsActive is true if this worker is permitted to take on jobs
	IsActive bool `form:"active" json:"active,omitempty" yaml:"active,omitempty" xml:"active"`

	// RequestedTTL is the desired TTL value
	RequestedTTL time.Duration `form:"requestedTTL,omitempty" json:"requestedTTL,omitempty" yaml:"requestedTTL,omitempty" xml:"requestedTTL,omitempty"`
}

type WorkerInstanceStatus struct {
	TTL time.Duration `form:"ttl,omitempty" json:"ttl,omitempty" yaml:"ttl,omitempty" xml:"ttl,omitempty"`
}

// RunnerSpec holds information about a runner as supplied by the administrator to register one
type RunnerSpec struct {
	// Description is a human readable text to describe intent behind this runner
	Description string `form:"description" json:"description,omitempty" yaml:"description,omitempty" xml:"description,omitempty"`

	// Requirements are optional to select sub-set of jobs this worker capable of taking
	Requirements manifest.LabelSelector `form:"requirements" json:"requirements,omitempty" yaml:"requirements,omitempty" xml:"requirements" gorm:"serializer:json"`

	// IsActive is true if this worker is permitted to take on jobs
	IsActive bool `form:"active" json:"active" yaml:"active" xml:"active"`

	// MaxInstances specifies maximum number of worker instance of this runner type. 0 means no limit
	MaxInstances uint64 `json:"maxInstance,omitempty" yaml:"maxInstance,omitempty"`
}

// RunnerStatus is information that owned and managed by the runner itself
type RunnerStatus struct {
	// Instances of this runner that are currently active
	NumberInstances uint64 `json:"numberInstances" yaml:"numberInstances" gorm:"-"`

	// Instances of this runner that are currently active
	Instances []WorkerInstance `json:"activeInstances,omitempty" yaml:"activeInstances,omitempty" gorm:"foreignKey:RunnerID;references:UID"`
}

// CronSchedule is a type to represent cron-like schedule: "@daily" or "0 */5 * * * *"
type CronSchedule string

type ScenarioSpec struct {
	// Description is a human readable text to describe the scenario
	Description string `form:"description" json:"description,omitempty" yaml:"description,omitempty" xml:"description"`

	// Requirements are optional to select sub-set of runners that are qualified to perform the script.
	Requirements manifest.LabelSelector `form:"requirements" json:"requirements,omitempty" yaml:"requirements,omitempty" xml:"requirements" gorm:"serializer:json"`

	// A schedule to run the script
	RunSchedule CronSchedule `form:"schedule" json:"schedule,omitempty" yaml:"schedule,omitempty" xml:"schedule"`

	// IsActive - scenario state: If false scenario will not be picked up for scheduling
	IsActive bool `form:"active" json:"active" yaml:"active" xml:"active"`

	// Script is the actual test scenario that a qualified runner executes
	Prob ProbManifest `form:"prob" json:"prob,omitempty" yaml:"prob,omitempty" xml:"prob" gorm:"serializer:json"`
}

// ComputeNextRun compute next point in time when a given Scenario can be scheduled to run
func (s *ScenarioSpec) ComputeNextRun(now time.Time) *time.Time {
	if s == nil || !s.IsActive || s.RunSchedule == "" {
		return nil
	}

	nextTime, err := gronx.NextTickAfter(string(s.RunSchedule), now, true)
	if err != nil {
		return nil
	}

	return &nextTime
}

// ScenarioStatus represents system computed state of the scenario resource
type ScenarioStatus struct {
	// Computed fields
	NextRun *time.Time `json:"nextScheduledRunTime,omitempty" yaml:"nextScheduledRunTime,omitempty" gorm:"-"`
	Results []Result   `json:"results,omitempty" yaml:"results,omitempty" gorm:"foreignKey:ScenarioID"`
}

// JobStatus represents a state of job: pending -> running -> completed | timeout | errored
type JobStatus string

// RunStatus represents the state of script execution once job has been successfully run
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
	RunNotFinished      RunStatus = ""
	RunFinishedSuccess  RunStatus = "success"
	RunFinishedFailed   RunStatus = "failed"
	RunFinishedError    RunStatus = "errored"
	RunFinishedCanceled RunStatus = "canceled"
	RunFinishedTimeout  RunStatus = "timeout"
)

type ResultSpec struct {
	ScenarioID manifest.ResourceID `json:"-" yaml:"-"`
	Scenario   Scenario            `json:"-" yaml:"-" gorm:"foreignKey:ScenarioID;references:UID"`

	// Timestamp when a job has been picked-up by a worked
	TimeStarted *time.Time `form:"start_time" json:"start_time" yaml:"start_time" xml:"start_time" gorm:"type:TIMESTAMP NULL"`

	// Timestamp when execution finished, if it finished
	TimeEnded *time.Time `form:"end_time" json:"end_time" yaml:"end_time" xml:"end_time" time_format:"unix" gorm:"type:TIMESTAMP NULL"`
}

type ResultStatus struct {
	// Status is the status of job in the job-scheduling life-cycle
	Status JobStatus `form:"status" json:"status" yaml:"status" xml:"status"  binding:"required"`

	// Result of the job execution, if job has been scheduled and finished one way or the other.
	Result RunStatus `form:"result" json:"result" yaml:"result" xml:"result"  binding:"required"`

	// Artifacts produced as a result of the job execution. Logs, traces, image, etc. depending on the nature of the Prob
	Artifacts []Artifact `json:"artifacts,omitempty" yaml:"artifacts,omitempty" gorm:"foreignKey:UID"`
}

type ArtifactSpec struct {
	// ExpireTime is a point in time after which the artifact can be removed by the system. If nil - artifact is 'pinned' and will not be purged, unless manually deleted.
	ExpireTime *time.Time `form:"expire_time,omitempty" json:"expire_time,omitempty" yaml:"expire_time,omitempty" xml:"expire_time,omitempty" time_format:"unix" gorm:"type:TIMESTAMP NULL"`

	// Relation type: log / HAR / etc? Determines how content is consumed by clients
	Rel string `form:"rel,omitempty" json:"rel,omitempty" yaml:"rel,omitempty" xml:"rel,omitempty"`

	// MimeType of the content
	MimeType string `form:"mimeType,omitempty" json:"mimeType,omitempty" yaml:"mimeType,omitempty" xml:"mimeType,omitempty"`

	// Blob content of the artifact
	Content []byte `form:"content,omitempty" json:"content,omitempty" yaml:"content,omitempty" xml:"content,omitempty"`
}

func (r Runner) ToManifest() manifest.ResourceManifest {
	return manifest.ToManifestWithStatus(manifest.StatefulResource[RunnerSpec, RunnerStatus](r))
}

func (r Scenario) ToManifest() manifest.ResourceManifest {
	return manifest.ToManifestWithStatus(manifest.StatefulResource[ScenarioSpec, ScenarioStatus](r))
}

func (r Artifact) ToManifest() manifest.ResourceManifest {
	return manifest.ToManifest(manifest.ResourceModel[ArtifactSpec](r))
}

func (r Result) ToManifest() manifest.ResourceManifest {
	return manifest.ToManifestWithStatus(manifest.StatefulResource[ResultSpec, ResultStatus](r))
}

func (r WorkerInstance) ToManifest() manifest.ResourceManifest {
	return manifest.ToManifestWithStatus(manifest.StatefulResource[WorkerInstanceSpec, WorkerInstanceStatus](r))
}

const (
	KindWorkerInstance manifest.Kind = "workerInstances"
	KindRunner         manifest.Kind = "runners"
	KindScenario       manifest.Kind = "scenarios"
	KindResult         manifest.Kind = "results"
	KindArtifact       manifest.Kind = "artifacts"
)

// WorkerInstance is an instance of Runner
type WorkerInstance manifest.StatefulResource[WorkerInstanceSpec, WorkerInstanceStatus]

// Runner is a manager resource that represents
// an instance of a worker capable of execution some probing job
type Runner manifest.StatefulResource[RunnerSpec, RunnerStatus]

// Scenario is a manager resource that represents
// an test case that can be executed and a history of such runs
type Scenario manifest.StatefulResource[ScenarioSpec, ScenarioStatus]

// Artifact model - produced and published by a runner,
// as a result of scenario execution.
type Artifact manifest.ResourceModel[ArtifactSpec]

// Result represents an outcome of a single execution of a given scenario
type Result manifest.StatefulResource[ResultSpec, ResultStatus]

func init() {
	manifest.MustRegisterManifest(KindWorkerInstance, &WorkerInstanceSpec{}, &WorkerInstanceStatus{})
	manifest.MustRegisterManifest(KindRunner, &RunnerSpec{}, &RunnerStatus{})
	manifest.MustRegisterManifest(KindResult, &ResultSpec{}, &ResultStatus{})
	manifest.MustRegisterManifest(KindScenario, &ScenarioSpec{}, &ScenarioStatus{})
	manifest.MustRegisterKind(KindArtifact, &ArtifactSpec{})
}

func NewWorkerInstance(m manifest.ResourceManifest) (WorkerInstance, error) {
	e, err := manifest.ManifestAsStatefulResource[WorkerInstanceSpec, WorkerInstanceStatus](m)
	entry := WorkerInstance(e)
	if err != nil {
		err = fmt.Errorf("failed to convert resource manifest into a Runner model: %w", err)
	}

	// validate metadata
	if entry.ObjectMeta.Name == "" {
		return entry, ErrResourceNameEmpty
	}

	if err := entry.ObjectMeta.Validate(); err != nil {
		return entry, err
	}

	return entry, err
}

func NewRunner(m manifest.ResourceManifest) (Runner, error) {
	e, err := manifest.ManifestAsStatefulResource[RunnerSpec, RunnerStatus](m)
	entry := Runner(e)
	if err != nil {
		err = fmt.Errorf("failed to convert resource manifest into a Runner model: %w", err)
	}

	// validate metadata
	if entry.ObjectMeta.Name == "" {
		return entry, ErrResourceNameEmpty
	}

	if err := entry.ObjectMeta.Validate(); err != nil {
		return entry, err
	}

	return entry, err
}

func NewResult(m manifest.ResourceManifest) (Result, error) {
	e, err := manifest.ManifestAsStatefulResource[ResultSpec, ResultStatus](m)
	entry := Result(e)
	if err != nil {
		err = fmt.Errorf("failed to convert resource manifest into a Result model: %w", err)
	}

	// validate metadata
	if entry.ObjectMeta.Name == "" {
		return entry, ErrResourceNameEmpty
	}

	if err := entry.ObjectMeta.Validate(); err != nil {
		return entry, err
	}

	return entry, err
}

func NewArtifact(m manifest.ResourceManifest) (Artifact, error) {
	e, err := manifest.ManifestAsResource[ArtifactSpec](m)
	entry := Artifact(e)
	if err != nil {
		err = fmt.Errorf("failed to convert resource manifest into an Artifact model: %w", err)
	}

	// validate metadata
	if entry.ObjectMeta.Name == "" {
		return entry, ErrResourceNameEmpty
	}

	if err := entry.ObjectMeta.Validate(); err != nil {
		return entry, err
	}

	return entry, err
}

func NewScenario(m manifest.ResourceManifest) (Scenario, error) {
	e, err := manifest.ManifestAsStatefulResource[ScenarioSpec, ScenarioStatus](m)
	entry := Scenario(e)
	if err != nil {
		return entry, fmt.Errorf("failed to convert resource manifest into a Scenario model: %w", err)
	}

	// validate metadata
	if entry.ObjectMeta.Name == "" {
		return entry, ErrResourceNameEmpty
	}

	if err := entry.ObjectMeta.Validate(); err != nil {
		return entry, err
	}

	return entry, err
}

type RunResultOption func(value *ResultStatus)

// func WithTime(value time.Time) RunResultOption {
// 	return func(result *ResultSpec) {
// 		result.TimeEnded = &value
// 	}
// }

func WithStatus(value JobStatus) RunResultOption {
	return func(result *ResultStatus) {
		result.Status = value
	}
}

func NewRunResults(runResult RunStatus, options ...RunResultOption) ResultStatus {
	result := ResultStatus{
		Status: JobCompleted,
		Result: runResult,
	}

	for _, option := range options {
		option(&result)
	}

	return result
}

func (u *Scenario) AfterFind(tx *gorm.DB) (err error) {
	u.Status.NextRun = u.Spec.ComputeNextRun(time.Now())
	if u.Status.Results == nil {
		err = tx.Model(u).Limit(1).Order("updated_at DESC").Association("Results").Find(&u.Status.Results)
		if err != nil {
			return
		}
	}

	return
}

func (u *Runner) AfterFind(tx *gorm.DB) (err error) {
	u.Status.NumberInstances = uint64(tx.Model(u).Association("Instances").Count())

	return
}
