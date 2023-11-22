package main

import (
	"context"
	"fmt"
	"log"
	"strconv"
	"time"

	"github.com/alecthomas/kong"
	"github.com/hibiken/asynq"

	"github.com/sre-norns/urth/pkg/grace"
	"github.com/sre-norns/urth/pkg/redqueue"
	"github.com/sre-norns/urth/pkg/runner"
	"github.com/sre-norns/urth/pkg/urth"
	"github.com/sre-norns/urth/pkg/wyrd"
)

type WorkerConfig struct {
	runner.RunnerConfig

	RedisAddress           string        `help:"Redis server address:port to connect to" default:"localhost:6379"`
	ApiRegistrationTimeout time.Duration `help:"Maximum time alloted for this worker to register with API server" default:"1m"`

	// isDone    bool
	apiClient urth.Service

	identity urth.Runner
}

func (w *WorkerConfig) labelJob(job urth.RunScenarioJob) wyrd.Labels {
	return wyrd.MergeLabels(
		w.SystemLabels,
		w.Labels,
		job.Labels,
		wyrd.Labels{
			"runner":             strconv.FormatUint(uint64(w.identity.ID), 10),     // Groups all artifacts produced by the same runner
			"runner.versioned":   w.identity.GetVersionedID().String(),              // Groups all artifacts produced by the same version of the scenario
			"scenario":           strconv.FormatUint(uint64(job.ScenarioID.ID), 10), // Groups all artifacts produced by the same scenario regardless of version
			"scenario.versioned": job.ScenarioID.String(),                           // Groups all artifacts produced by the same version of the scenario
			"scenario.kind":      string(job.Script.Kind),                           // Groups all artifacts produced by the type of script: TCP probe, HTTP probe, etc.
		},
	)
}

// HandleWelcomeEmailTask handler for welcome email task.
func (w *WorkerConfig) handleRunScenarioTask(ctx context.Context, t *asynq.Task) error {
	log.Println("New job execution request")

	job, err := redqueue.UnmarshalJob(t)
	if err != nil {
		log.Println("Failed to deserialize message content: ", err)
		// TODO: Log and count metrics
		return err // Note: job can be re-tried
	}

	timeStarted := time.Now()
	scenarioName := fmt.Sprintf("%v-%v", job.Name, job.ScenarioID)
	runID := fmt.Sprintf("%v-%v", scenarioName, timeStarted.UnixMicro())
	log.Println("jobID:", runID)

	// TODO: Check requirements!

	resultsApiClient := w.apiClient.GetResultsAPI(job.ScenarioID.ID)
	// Notify API-server that a job has been accepted by this worker
	// FIXME: Worker must use its credentials jwt
	runCreated, err := resultsApiClient.Create(ctx, urth.CreateScenarioRunResults{
		CreateResourceMeta: urth.CreateResourceMeta{
			Name:   runID, // Note: not unique!
			Labels: w.labelJob(job),
		},
		InitialScenarioRunResults: urth.InitialScenarioRunResults{
			ScenarioID:  job.ScenarioID,
			RunnerID:    w.identity.GetVersionedID(),
			TimeStarted: timeStarted,
		},
	})
	if err != nil {
		log.Printf("failed to register new run %q: %v", runID, err)
		// TODO: Log and count metrics
		return err // Note: job can be re-tried
	}

	timeout := w.Timeout
	if (job.Script != nil && job.Script.Timeout != 0) && timeout > job.Script.Timeout {
		timeout = job.Script.Timeout
	}
	workCtx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()
	log.Println("jobID: ", runID, ", starting timeout:", timeout)

	runResult, artifacts, err := runner.Play(workCtx, job.Script, runner.RunOptions{
		Http: runner.HttpOptions{
			CaptureResponseBody: false,
			CaptureRequestBody:  false,
			IgnoreRedirects:     false,
		},
		Puppeteer: runner.PuppeteerOptions{
			Headless:         true,
			WorkingDirectory: w.WorkingDirectory,
			TempDirPrefix:    fmt.Sprintf("run-%v", scenarioName),
			KeepTempDir:      job.IsKeepDirectory,
		},
	})
	if err != nil {
		log.Printf("failed to run the job %q: %v", runID, err)
	}

	// Push artifacts if any:
	wg := grace.NewWorkgroup(4)

	artifactsApiClient := w.apiClient.GetArtifactsApi()
	for _, artifact := range artifacts {
		wg.Go(func() error {
			_, err := artifactsApiClient.Create(ctx, urth.CreateArtifactRequest{
				CreateResourceMeta: urth.CreateResourceMeta{
					Name: fmt.Sprintf("%v.%v", runID, artifact.Rel),
					Labels: wyrd.MergeLabels(
						w.labelJob(job),
						wyrd.Labels{
							"artifact.kind": artifact.Rel,                                  // Groups all artifacts produced by the content type: logs / HAR / etc
							"run":           strconv.FormatUint(uint64(runCreated.ID), 10), // Groups all artifacts produced in the same run
						},
					),
				},
				ScenarioRunResultsID: job.ScenarioID.ID,
				ArtifactValue:        artifact,
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
		created, err := resultsApiClient.Update(ctx, runCreated.VersionedResourceId, runCreated.Token, runResult)
		if err != nil {
			log.Printf("failed to post run results for %q: %v", runID, err)
			return err // TODO: retry or not? Add results into the retry queue to post later?
		}

		log.Printf("job %q resultsID: %v", runID, created.VersionedResourceId)
		return nil
	})

	wg.Wait()
	log.Printf("job %q competed: %v", runID, runResult.Result)
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

	// TODO: Check that working directory exists and writable!
	grace.ExitOrLog(runner.SetupRunEnv(defaultConfig.WorkingDirectory))

	// Create a new task's mux instance.
	mux := asynq.NewServeMux()
	mux.HandleFunc(
		urth.RunScenarioTopicName,           // task type
		defaultConfig.handleRunScenarioTask, // handler function
	)

	// for !defaultConfig.isDone {
	rego := urth.RunnerRegistration{
		IsOnline:       true,
		InstanceLabels: defaultConfig.Labels,
	}

	regoCtx, cancel := context.WithTimeout(context.Background(), defaultConfig.ApiRegistrationTimeout)
	defer cancel()

	defaultConfig.identity, err = apiClient.GetRunnerAPI().Auth(regoCtx, urth.ApiToken(defaultConfig.ApiToken), rego)
	if err != nil {
		// TODO: Should be back-off
		appCtx.FatalIfErrorf(err)
		return
	}
	log.Print("Registered with API server as: ", defaultConfig.identity.Name, "Id:", defaultConfig.identity.GetVersionedID())

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
