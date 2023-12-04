package main

import (
	"context"
	"fmt"
	"log"
	"time"

	"github.com/alecthomas/kong"
	"github.com/hibiken/asynq"

	"github.com/sre-norns/urth/pkg/grace"
	"github.com/sre-norns/urth/pkg/redqueue"
	"github.com/sre-norns/urth/pkg/runner"
	"github.com/sre-norns/urth/pkg/urth"
	"github.com/sre-norns/urth/pkg/wyrd"

	_ "github.com/sre-norns/urth/pkg/probers/har_prob"
	_ "github.com/sre-norns/urth/pkg/probers/http_prob"
	_ "github.com/sre-norns/urth/pkg/probers/puppeteer_prob"
	_ "github.com/sre-norns/urth/pkg/probers/pypuppeteer_prob"
	_ "github.com/sre-norns/urth/pkg/probers/tcp_prob"
)

type WorkerConfig struct {
	runner.RunnerConfig

	RedisAddress           string        `help:"Redis server address:port to connect to" default:"localhost:6379"`
	ApiRegistrationTimeout time.Duration `help:"Maximum time alloted for this worker to register with API server" default:"1m"`

	apiClient urth.Service

	identity urth.Runner
}

// HandleWelcomeEmailTask handler for welcome email task.
func (w *WorkerConfig) handleRunScenarioTask(ctx context.Context, t *asynq.Task) error {
	messageId := t.ResultWriter().TaskID()
	log.Print("New job execution request: ", messageId)

	job, err := redqueue.UnmarshalJob(t)
	if err != nil {
		log.Print("Failed to deserialize message content: ", err)
		// TODO: Log and count metrics
		return err // Note: job can be re-tried
	}

	timeout := w.Timeout
	if (job.Script != nil && job.Script.Timeout != 0) && timeout > job.Script.Timeout {
		timeout = job.Script.Timeout
	}

	runID := job.RunName
	log.Print("jobID: ", runID)

	// TODO: Check requirements!

	resultsApiClient := w.apiClient.GetResultsAPI(job.ScenarioID.ID)
	// Notify API-server that a job has been accepted by this worker
	// FIXME: Worker must use its credentials jwt
	// Authorize this worker to pick up this job:
	log.Print("jobID: ", runID, " requesting authorization to execute")
	runAuth, err := resultsApiClient.Auth(ctx, job.RunID, urth.AuthRunRequest{
		RunnerID: w.identity.GetVersionedID(),
		Timeout:  timeout,
		Labels: wyrd.MergeLabels(
			w.LabelJob(w.identity.GetVersionedID(), job),
			wyrd.Labels{
				urth.LabelScenarioRunMessageId: messageId,
			},
		),
	})

	if err != nil {
		log.Printf("failed to register new run %q: %v", runID, err)
		// TODO: Log and count metrics
		return err // Note: job can be re-tried
	}

	workCtx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()
	log.Print("jobID: ", runID, ", starting timeout: ", timeout)

	runResult, artifacts, err := runner.Play(workCtx, job.Script, runner.RunOptions{
		Http: runner.HttpOptions{
			CaptureResponseBody: false,
			CaptureRequestBody:  false,
			IgnoreRedirects:     false,
		},
		Puppeteer: runner.PuppeteerOptions{
			Headless:         true,
			WorkingDirectory: w.WorkingDirectory,
			TempDirPrefix:    job.RunName,
			KeepTempDir:      job.IsKeepDirectory,
		},
	})
	if err != nil {
		log.Printf("failed to run the job %q: %v", runID, err)
		// Note this error - does not abort the task as details(logs and status) must be posted back to the server
	}

	// Push artifacts if any:
	wg := grace.NewWorkgroup(4)

	artifactsApiClient := w.apiClient.GetArtifactsApi()
	for _, a := range artifacts {
		artifact := a
		wg.Go(func() error {
			// TODO: Must include run Auth Token
			_, err := artifactsApiClient.Create(ctx, wyrd.ResourceManifest{
				Metadata: wyrd.ObjectMeta{
					Name: fmt.Sprintf("%v.%v", runID, artifact.Rel),
					Labels: wyrd.MergeLabels(
						w.LabelJob(w.identity.GetVersionedID(), job),
						wyrd.Labels{
							urth.LabelScenarioArtifactKind: artifact.Rel,          // Groups all artifacts produced by the content type: logs / HAR / etc
							urth.LabelScenarioRunId:        job.RunID.ID.String(), // Groups all artifacts produced in the same run
							urth.LabelScenarioRunMessageId: messageId,
						},
					),
				},
				Spec: artifact,
			})

			if err != nil {
				log.Printf("failed to post artifact %q for %q: %v", artifact.Rel, runID, err)
				return err // TODO: retry or not? Add results into the retry queue to post later?
			}

			return nil
		})
	}

	// Notify API-server that the job has been complete
	wg.Go(func() error {
		created, err := resultsApiClient.Update(ctx, runAuth.VersionedResourceId, runAuth.Token, runResult)
		if err != nil {
			log.Printf("failed to post run results for %q: %v", runID, err)
			return err // TODO: retry or not? Add results into the retry queue to post later?
		}

		log.Print("jobID: ", runID, ", resultID: ", created.VersionedResourceId)
		return nil
	})

	wg.Wait()
	log.Print("jobID: ", runID, ", competed: ", runResult.Result)
	return nil
}

var defaultConfig = WorkerConfig{
	RunnerConfig: runner.NewDefaultConfig(),
}

func main() {
	appCtx := kong.Parse(&defaultConfig,
		kong.Name("runner"),
		kong.Description("Urth async worker picks up jobs from and executes scripts, producing metrics and test artifacts"),
	)

	apiClient, err := urth.NewRestApiClient(defaultConfig.ApiServerAddress)
	if err != nil {
		log.Fatalf("Failed to initialize API Client: %v", err)
		return
	}
	if apiClient == nil {
		log.Fatalf("Initialize of API Client failed: unexpected `nil` value returned")
		return
	}

	defaultConfig.apiClient = apiClient

	// Create a new task's mux instance.
	mux := asynq.NewServeMux()
	mux.HandleFunc(
		urth.RunScenarioTopicName,           // task type
		defaultConfig.handleRunScenarioTask, // handler function
	)

	regoCtx, cancel := context.WithTimeout(context.Background(), defaultConfig.ApiRegistrationTimeout)
	defer cancel()

	defaultConfig.identity, err = apiClient.GetRunnerAPI().Auth(regoCtx, urth.ApiToken(defaultConfig.ApiToken), urth.RunnerRegistration{
		IsOnline:       true,
		InstanceLabels: defaultConfig.GetEffectiveLabels(),
	})
	if err != nil {
		// TODO: Should be back-off and retry
		appCtx.FatalIfErrorf(err)
		return
	}
	log.Print("Registered with API server as: ", defaultConfig.identity.Name, "Id: ", defaultConfig.identity.GetVersionedID())

	// Create and configuring Redis connection.
	redisConnection := asynq.RedisClientOpt{
		Addr: defaultConfig.RedisAddress, // Redis server address
	}

	// Create and Run Asynq worker server.
	workerServer := asynq.NewServer(redisConnection, asynq.Config{
		// BaseContext: func() context.Context { return mainContext },
	})

	appCtx.FatalIfErrorf(workerServer.Run(mux))
}
