package redqueue

import (
	"context"
	"fmt"
	"log"
	"sync/atomic"

	"github.com/hibiken/asynq"
	"github.com/sre-norns/urth/pkg/urth"
)

const TaskType = urth.RunScenarioTopicName

var ErrInvalidJobSpec = fmt.Errorf("job spec if nil")

func UnmarshalJob(msg *asynq.Task) (urth.RunScenarioJob, error) {
	return urth.UnmarshalJob(msg.Payload())
}

func MarshalJob(job urth.RunScenarioJob) (*asynq.Task, error) {
	data, err := urth.MarshalJob(job)
	if err != nil {
		return nil, err
	}

	return asynq.NewTask(TaskType, data), nil
}

func NewScheduler(ctx context.Context, redisAddr string) (urth.Scheduler, error) {
	client := asynq.NewClient(asynq.RedisClientOpt{Addr: redisAddr})

	return &asynqScheduler{
		client: client,
	}, nil
}

type asynqScheduler struct {
	// wg             sync.WaitGroup
	totalErrors    uint64
	totalRunnables uint64

	client *asynq.Client
}

func (s *asynqScheduler) Close() error {
	if s == nil || s.client == nil {
		return nil
	}

	return s.client.Close()
}

func (s *asynqScheduler) Schedule(ctx context.Context, job urth.RunScenarioJob) (urth.RunId, error) {
	if job.Prob.Spec == nil {
		return urth.InvalidRunId, fmt.Errorf("can't schedule job: %w", ErrInvalidJobSpec)
	}

	task, err := MarshalJob(job)
	if err != nil {
		log.Printf("Scheduling error %v, will try again later", err)
		atomic.AddUint64(&s.totalErrors, 1)
		return urth.InvalidRunId, err
	}

	atomic.AddUint64(&s.totalRunnables, 1)
	info, err := s.client.Enqueue(task, asynq.MaxRetry(1))
	if err != nil {
		log.Printf("Failed to publish: %v", err)
		atomic.AddUint64(&s.totalErrors, 1)

		return urth.InvalidRunId, err
	}

	log.Printf("published task: %v", info.ID)
	return urth.RunId(info.ID), err
}
