package main

import (
	"context"
	"fmt"
	"log"
	"os"
	"os/user"
	"reflect"
	"strings"
	"time"

	"github.com/alecthomas/kong"
	"github.com/hibiken/asynq"
	_ "github.com/joho/godotenv/autoload"

	"github.com/sre-norns/urth/pkg/prob"
	"github.com/sre-norns/urth/pkg/redqueue"
	"github.com/sre-norns/urth/pkg/runner"
	"github.com/sre-norns/urth/pkg/urth"
	"github.com/sre-norns/wyrd/pkg/grace"
	"github.com/sre-norns/wyrd/pkg/manifest"
)

type WorkerConfig struct {
	urth.APIClientConfig `embed:"" prefix:"client."`
	runner.RunnerConfig  `embed:"" `

	RedisAddress           string                `help:"Redis server address:port to connect to" default:"localhost:6379"`
	APIRegistrationTimeout time.Duration         `help:"Maximum time alloted for this worker to register with API server" default:"1m"`
	Name                   manifest.ResourceName `help:"Custom name for this worker" env:"WORKER_NAME"`

	apiClient urth.Service

	runner    urth.Runner
	identityT *urth.WorkerInstance
}

// HandleWelcomeEmailTask handler for welcome email task.
func (w *WorkerConfig) handleRunScenarioTask(ctx context.Context, t *asynq.Task) error {
	messageID := t.ResultWriter().TaskID()
	log.Print("New job execution request: ", messageID)

	job, err := redqueue.UnmarshalJob(t)
	if err != nil {
		log.Print("Failed to deserialize message content: ", err)
		log.Println(string(t.Payload()))
		// TODO: Log and count metrics
		return err // Note: job can be re-tried
	}

	// FIXME: Check job.prob != nil
	timeout := w.RunnerConfig.Timeout
	if (job.Prob.Timeout != 0) && timeout > job.Prob.Timeout {
		timeout = job.Prob.Timeout
	}

	// TODO: Should a worker check requirements?

	resultsAPIClient := w.apiClient.Results(job.ScenarioName)
	// Notify API-server that a job has been accepted by this worker
	// FIXME: Worker must use its credentials jwt
	// Authorize this worker to pick up this job:
	log.Print("requesting authorization to execute jobID: ", job.ResultName)
	// This is the asynq prototype worker the deprecated Auth is retained for; the
	// NATS runner uses the session-backed ClaimRun instead.
	//lint:ignore SA1019 deliberate: this is the prototype worker the deprecated Auth is retained for.
	runAuth, err := resultsAPIClient.Auth(ctx,
		job.ResultName,
		urth.AuthJobRequest{
			WorkerID: w.identityT.GetVersionedID(),
			RunnerID: w.runner.GetVersionedID(),
			Timeout:  timeout,
			// Present worker's capabilities
			Labels: manifest.MergeLabels(
				w.LabelJob(w.runner.ObjectMeta, w.identityT.ObjectMeta, job),
				manifest.Labels{
					urth.LabelResultMessageID: messageID,
				},
			),
		})
	if err != nil {
		log.Printf("failed to register new run of %q: %v", job.ResultName, err)
		// TODO: Log and count metrics
		return err // Note: job can be re-tried
	}

	workCtx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	log.Print("jobID: ", job.ResultName, ", kind: ", job.Prob.Kind, ", timeout: ", timeout, ", type: ", reflect.TypeOf(job.Prob.Spec))
	runResult, artifacts, err := runner.Play(workCtx,
		job.Prob,
		prob.RunOptions{
			HTTP: prob.HTTPOptions{
				CaptureResponseBody: false,
				CaptureRequestBody:  false,
				IgnoreRedirects:     false,
			},
			Puppeteer: prob.PuppeteerOptions{
				Headless:         true, // TODO: Should be config option
				WorkingDirectory: w.WorkingDirectory,
				TempDirPrefix:    string(job.ResultName),
				KeepTempDir:      job.IsKeepDirectory,
			},
		})
	if err != nil {
		log.Printf("failed to run the job %q: %v", job.ResultName, err)
		// Note this error - does not abort the task as details(logs and status) must be posted back to the server
	}

	// Push artifacts if any:
	wg := grace.NewWorkgroup(4)

	artifactsAPIClient := w.apiClient.Artifacts()
	for _, a := range artifacts {
		artifact := a
		wg.Go(func() error {
			// TODO: Must include run Auth Token
			_, err := artifactsAPIClient.Create(ctx,
				runAuth.Token,
				manifest.ResourceManifest{
					TypeMeta: manifest.TypeMeta{
						APIVersion: "v1",
						Kind:       urth.KindArtifact,
					},
					Metadata: manifest.ObjectMeta{
						Name: manifest.ResourceName(fmt.Sprintf("%v.%v", job.ResultName, artifact.Artifact.Rel)),
						Labels: manifest.MergeLabels(
							w.LabelJob(w.runner.ObjectMeta, w.identityT.ObjectMeta, job),
							manifest.Labels{
								urth.LabelResultMessageID: messageID,
							},
						),
					},
					Spec: artifact,
				},
			)

			if err != nil {
				log.Printf("failed to post artifact %q for %q: %v", artifact.Artifact.Rel, job.ResultName, err)
				return err // TODO: retry or not? Add results into the retry queue to post later?
			}

			return nil
		})
	}

	// Notify API-server that the job has been complete
	wg.Go(func() error {
		created, err := resultsAPIClient.UpdateStatus(ctx, runAuth.VersionedResourceID, runAuth.Token, runResult)
		if err != nil {
			log.Printf("failed to post run results for %q: %v", job.ResultName, err)
			return err // TODO: retry or not? Add results into the retry queue to post later?
		}

		log.Print("jobID: ", job.ResultName, ", resultID: ", created.VersionedResourceID)
		return nil
	})

	wg.Wait()
	log.Print("jobID: ", job.ResultName, ", competed: ", runResult.Result)
	return nil
}

var appConfig = WorkerConfig{
	RunnerConfig: runner.NewDefaultConfig(),
}

func generateName() manifest.ResourceName {
	name := ""
	if uname, err := user.Current(); err == nil && manifest.ValidateSubdomainName(uname.Name) == nil {
		name = uname.Name
	}

	if hostname, err := os.Hostname(); err == nil && manifest.ValidateSubdomainName(hostname) == nil {
		if name != "" {
			name += "."
		}

		name += hostname
	}

	// If produced name is still not valid, generate a random one
	if manifest.ValidateSubdomainName(name) != nil {
		name = string(urth.NewRandToken(16))
	}

	return manifest.ResourceName(strings.ToLower(name))
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
		appConfig.Name = generateName()
	}

	apiClient, err := appConfig.NewClient()
	grace.SuccessRequired(err, "Failed to initialize API Client")

	appConfig.apiClient = apiClient

	regoCtx, cancel := context.WithTimeout(context.Background(), appConfig.APIRegistrationTimeout)
	defer cancel()

	// Request Auth to join the workers queue.
	// This is the asynq prototype worker the deprecated Auth is retained for; it
	// finds its identity in the returned runner manifest. AuthWorker is the
	// session-backed replacement used by the NATS runner.
	//lint:ignore SA1019 deliberate: this is the prototype worker the deprecated Auth is retained for.
	identity, err := apiClient.Runners().Auth(regoCtx,
		appConfig.Token,
		urth.WorkerInstance{
			ObjectMeta: manifest.ObjectMeta{
				Name:   appConfig.Name,
				Labels: appConfig.GetEffectiveLabels(),
			},
			Spec: urth.WorkerInstanceSpec{
				RequestedTTL: 0,
			},
		}.ToManifest())

	// TODO: Should be back-off and retry ?
	grace.SuccessRequired(err, "failed to Auth to the Runner API")

	appConfig.runner, err = urth.NewRunner(identity)
	grace.SuccessRequired(err, "Auth API returner unexpected type")

	if len(appConfig.runner.Status.Instances) == 0 {
		log.Fatal("returned Runner identity does not contain worker rego, abort")
	} else {
		appConfig.identityT = &appConfig.runner.Status.Instances[0]
	}

	log.Print("Registered with API server as Runner: ", appConfig.runner.Name,
		", Id: ", appConfig.runner.GetVersionedID(),
	)
	log.Printf("...Worker ID: %q, Name: %q", appConfig.identityT.GetVersionedID(), appConfig.identityT.Name)

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
