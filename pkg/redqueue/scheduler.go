package redqueue

import (
	"context"
	"log"
	"sync/atomic"

	"github.com/hibiken/asynq"
	"github.com/sre-norns/urth/pkg/urth"
)

const TaskType = "scenario:run"

func UnmarshalJob(msg *asynq.Task) (urth.RunScenarioJob, error) {
	return urth.UnmarshalJobYAML(msg.Payload())
}

func MarshalJob(job urth.RunScenarioJob) (*asynq.Task, error) {
	data, err := urth.MarshalJobYAML(job)
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
	// if !scenario.IsActive {
	// 	return urth.InvalidRunId, nil
	// }

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
	}
	log.Printf("published task: %v", info.ID)
	return urth.RunId(info.ID), err
}
