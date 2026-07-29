package natsq

import (
	"context"
	"errors"
	"fmt"

	"github.com/sre-norns/urth/pkg/urth"
	"github.com/sre-norns/wyrd/pkg/manifest"
)

// ObserveRunnerChannel implements urth.RunnerChannelObserver.
//
// It reads the same consumer state JetStreamCollector scrapes for metrics, and
// for a related purpose: the metrics answer "is this fleet keeping up" over
// time, while this answers "is anyone at this queue right now" for one runner an
// operator happens to be looking at.
//
// NumWaiting is the interesting number. A pull consumer's waiting count is the
// pull requests workers have parked and not yet had filled, so a non-zero value
// means at least one worker is bound, connected and asking -- established
// without any cooperation from the worker itself, which is what makes it a
// useful check on the presence workers report about themselves.
func (s *scheduler) ObserveRunnerChannel(ctx context.Context, runnerUID manifest.ResourceID) (urth.RunnerChannelStatus, error) {
	consumer, err := BindRunnerConsumer(ctx, s.js, runnerUID)
	if errors.Is(err, ErrNoConsumer) {
		// The runner has no queue yet -- nothing has registered against it. Not
		// observed, and not a failure: there is simply nothing to report.
		return urth.RunnerChannelStatus{}, nil
	}
	if err != nil {
		return urth.RunnerChannelStatus{}, err
	}

	info, err := consumer.Info(ctx)
	if err != nil {
		return urth.RunnerChannelStatus{}, fmt.Errorf("failed to read the queue state of runner %q: %w", runnerUID, err)
	}

	return urth.RunnerChannelStatus{
		Observed:   true,
		Pullers:    info.NumWaiting,
		Pending:    info.NumPending,
		AckPending: info.NumAckPending,
	}, nil
}
