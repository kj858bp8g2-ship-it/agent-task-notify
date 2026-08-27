package worker

import (
	"context"
	"time"
)

type Result struct{ Accepted, Retryable bool }
type Report struct {
	MainAttempts                                    int
	Accepted, ExtensionAttempted, ExtensionAccepted bool
}
type Attempt func(context.Context, bool) Result
type Sleep func(context.Context, time.Duration) error

var retryDelays = []time.Duration{5 * time.Second, 15 * time.Second, 30 * time.Second, 60 * time.Second}

// Deliver bounds primary retries and sends at most one extension after acceptance.
func Deliver(ctx context.Context, ringSeconds int, continuous bool, send Attempt, sleep Sleep) Report {
	var report Report
	if ctx == nil || send == nil || ringSeconds < 30 || ringSeconds > 60 {
		return report
	}
	if sleep == nil {
		sleep = sleepContext
	}
	for attempt := 0; attempt <= len(retryDelays); attempt++ {
		if ctx.Err() != nil {
			return report
		}
		report.MainAttempts++
		result := send(ctx, false)
		if result.Accepted {
			report.Accepted = true
			break
		}
		if !result.Retryable || attempt == len(retryDelays) || ctx.Err() != nil {
			return report
		}
		if sleep(ctx, retryDelays[attempt]) != nil {
			return report
		}
	}
	if report.Accepted && continuous && ringSeconds > 30 && ctx.Err() == nil {
		if sleep(ctx, time.Duration(ringSeconds-30)*time.Second) != nil || ctx.Err() != nil {
			return report
		}
		report.ExtensionAttempted = true
		report.ExtensionAccepted = send(ctx, true).Accepted
	}
	return report
}

func sleepContext(ctx context.Context, delay time.Duration) error {
	timer := time.NewTimer(delay)
	defer timer.Stop()
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-timer.C:
		return nil
	}
}
