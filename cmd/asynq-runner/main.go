package main

import (
	"context"
	"fmt"
	"log"
	"os"
	"os/user"
	"time"

	"github.com/alecthomas/kong"
	"github.com/hibiken/asynq"

	"github.com/sre-norns/urth/pkg/redqueue"
	"github.com/sre-norns/urth/pkg/runner"
	"github.com/sre-norns/urth/pkg/urth"
	"github.com/sre-norns/wyrd/pkg/grace"
	"github.com/sre-norns/wyrd/pkg/manifest"

	_ "github.com/sre-norns/urth/pkg/probers/har"
	_ "github.com/sre-norns/urth/pkg/probers/http"
	_ "github.com/sre-norns/urth/pkg/probers/puppeteer"
	_ "github.com/sre-norns/urth/pkg/probers/pypuppeteer"
	_ "github.com/sre-norns/urth/pkg/probers/tcp"
)

type WorkerConfig struct {
	urth.ApiClientConfig `embed:"" prefix:"client."`
	runner.RunnerConfig  `embed:"" `

	RedisAddress           string        `help:"Redis server address:port to connect to" default:"localhost:6379"`
	ApiRegistrationTimeout time.Duration `help:"Maximum time alloted for this worker to register with API server" default:"1m"`
	Name                   string        `help:"Custom name for this worker" env:"WORKER_NAME"`

	apiClient urth.Service
	identity  urth.Runner
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

	// FIXME: Check job.prob != nil
	timeout := w.RunnerConfig.Timeout
	if (job.Prob.Timeout != 0) && timeout > job.Prob.Timeout {
		timeout = job.Prob.Timeout
	}

	runID := job.ResultName
	log.Print("jobID: ", runID)

	// TODO: Check requirements!

	resultsApiClient := w.apiClient.GetResultsAPI(job.ScenarioName)
	// Notify API-server that a job has been accepted by this worker
	// FIXME: Worker must use its credentials jwt
	// Authorize this worker to pick up this job:
	log.Print("jobID: ", runID, " requesting authorization to execute")
	runAuth, err := resultsApiClient.Auth(ctx, job.ResultName, urth.AuthJobRequest{
		WorkerID: w.identity.Status.Instances[0].GetVersionedID(),
		RunnerID: w.identity.GetVersionedID(),
		Timeout:  timeout,
		Labels: manifest.MergeLabels(
			w.LabelJob(w.identity.Name, w.identity.GetVersionedID(), job),
			manifest.Labels{
				urth.LabelRunResultsMessageId: messageId,
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

	runResult, artifacts, err := runner.Play(workCtx, job.Prob, runner.RunOptions{
		Http: runner.HttpOptions{
			CaptureResponseBody: false,
			CaptureRequestBody:  false,
			IgnoreRedirects:     false,
		},
		Puppeteer: runner.PuppeteerOptions{
			Headless:         true,
			WorkingDirectory: w.WorkingDirectory,
			TempDirPrefix:    string(job.ResultName),
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
			_, err := artifactsApiClient.Create(ctx, manifest.ResourceManifest{
				TypeMeta: manifest.TypeMeta{
					Kind: urth.KindArtifact,
				},
				Metadata: manifest.ObjectMeta{
					Name: manifest.ResourceName(fmt.Sprintf("%v.%v", runID, artifact.Rel)),
					Labels: manifest.MergeLabels(
						w.LabelJob(w.identity.Name, w.identity.GetVersionedID(), job),
						manifest.Labels{
							urth.LabelScenarioArtifactKind: artifact.Rel, // Groups all artifacts produced by the content type: logs / HAR / etc
							urth.LabelRunResultsMessageId:  messageId,
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
		created, err := resultsApiClient.Update(ctx, runAuth.VersionedResourceID, runAuth.Token, runResult)
		if err != nil {
			log.Printf("failed to post run results for %q: %v", runID, err)
			return err // TODO: retry or not? Add results into the retry queue to post later?
		}

		log.Print("jobID: ", runID, ", resultID: ", created.VersionedResourceID)
		return nil
	})

	wg.Wait()
	log.Print("jobID: ", runID, ", competed: ", runResult.Result)
	return nil
}

var appConfig = WorkerConfig{
	RunnerConfig: runner.NewDefaultConfig(),
}

func main() {
	log.SetFlags(0)

	appCtx := kong.Parse(&appConfig,
		kong.Name("runner"),
		kong.Description("Urth async worker picks up jobs from and executes scripts, producing metrics and test artifacts"),
	)

	if appConfig.Token == "" {
		grace.SuccessRequired(fmt.Errorf("no token provided"), "Auth token required")
	}

	if appConfig.Name == "" {
		if uname, err := user.Current(); err == nil && manifest.ValidateSubdomainName(uname.Name) == nil {
			appConfig.Name = uname.Name
		}

		if name, err := os.Hostname(); err == nil && manifest.ValidateSubdomainName(name) == nil {
			if appConfig.Name != "" {
				appConfig.Name += "."
			}

			appConfig.Name += name
		}

		if appConfig.Name == "" {
			appConfig.Name = string(urth.NewRandToken(16))
		}
	}

	apiClient, err := appConfig.NewClient()
	grace.SuccessRequired(err, "Failed to initialize API Client")

	appConfig.apiClient = apiClient

	regoCtx, cancel := context.WithTimeout(context.Background(), appConfig.ApiRegistrationTimeout)
	defer cancel()

	// Request Auth to join the workers queue
	identity, err := apiClient.GetRunnerAPI().Auth(regoCtx, appConfig.Token, urth.WorkerInstance{
		ObjectMeta: manifest.ObjectMeta{
			Name:   manifest.ResourceName(appConfig.Name),
			Labels: appConfig.GetEffectiveLabels(),
		},
		Spec: urth.WorkerInstanceSpec{
			IsActive:     false,
			RequestedTTL: 0,
		},
	}.ToManifest())

	// TODO: Should be back-off and retry ?
	grace.SuccessRequired(err, "failed to Auth to the Runner API")

	appConfig.identity, err = urth.NewRunner(identity)
	grace.SuccessRequired(err, "Auth API returner unexpected type")

	if len(appConfig.identity.Status.Instances) == 0 {
		log.Fatal("returned Runner identity does not contain worker rego, abort")
	}

	log.Print("Registered with API server as Runner: ", appConfig.identity.Name,
		", Id: ", appConfig.identity.GetVersionedID(),
	)
	log.Printf("...Worker ID: %q, Name: %q", appConfig.identity.Status.Instances[0].GetVersionedID(), appConfig.identity.Status.Instances[0].Name)

	// Create and configuring Redis connection.
	redisConnection := asynq.RedisClientOpt{
		Addr: appConfig.RedisAddress, // Redis server address
	}

	// Create and Run Asynq worker server.
	workerServer := asynq.NewServer(redisConnection, asynq.Config{
		// BaseContext: func() context.Context { return mainContext },
	})

	// Create a new task's mux instance.
	mux := asynq.NewServeMux()
	mux.HandleFunc(
		urth.RunScenarioTopicName,       // task type
		appConfig.handleRunScenarioTask, // handler function
	)

	appCtx.FatalIfErrorf(workerServer.Run(mux))
}
