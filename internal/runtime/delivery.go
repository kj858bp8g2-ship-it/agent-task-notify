package runtime

import (
	"context"
	"time"

	"github.com/kj858bp8g2-ship-it/agent-task-notify/internal/core"
	"github.com/kj858bp8g2-ship-it/agent-task-notify/internal/secrets"
	"github.com/kj858bp8g2-ship-it/agent-task-notify/internal/worker"
)

func (r *Runtime) RunJob(ctx context.Context, key string) error {
	if !r.usable(ctx) || !core.ValidKey(key) || r.Send == nil {
		return errUnavailable
	}
	release, err := r.acquire(ctx, "job", key)
	if err != nil {
		return err
	}
	// Delivery never waits on a session lock. Retention runs only after release.
	defer func() { release(); r.cleanup(ctx) }()
	j, found, err := r.readJob(key)
	if err != nil {
		return err
	}
	if !found {
		return errUnavailable
	}
	save := func() error { return writeRecord(r.path("jobs", key), j) }
	if j.Status != "pending" || j.Attempts > 0 {
		changed := false
		if j.Status == "sending" || (j.Status == "pending" && j.Attempts > 0) {
			j.Diagnostic = "ambiguous"
			changed = true
		}
		if j.ExtensionStatus == "pending" || j.ExtensionStatus == "sending" {
			j.ExtensionStatus = "ambiguous"
			j.ExtensionDiagnostic = "ambiguous"
			changed = true
		}
		if changed {
			if err := save(); err != nil {
				return err
			}
		}
		if j.Status == "sent" {
			return nil
		}
		return errDelivery
	}
	// A failed process launch is not a background recovery queue.
	if j.Diagnostic == "spawn-failed" {
		return errDelivery
	}
	credential, err := r.Repository.Credential(j.Settings.Provider, secrets.Background)
	if err != nil {
		j.Status = "failed"
		j.Diagnostic = "credential"
		j.FinishedAt = stamp(r.Now())
		if err := save(); err != nil {
			return err
		}
		return errCredential
	}
	continuous := j.Settings.Provider == "bark" && j.Settings.Continuous
	var persistenceErr error
	var acceptedAt time.Time
	attempt := func(deliveryContext context.Context, extension bool) worker.Result {
		if persistenceErr != nil {
			return worker.Result{}
		}
		if extension {
			j.ExtensionStatus = "sending"
			j.ExtensionAttempted = true
		} else {
			j.Status = "sending"
			j.Attempts++
			j.Diagnostic = ""
		}
		if persistenceErr = save(); persistenceErr != nil {
			return worker.Result{}
		}
		result := r.Send(deliveryContext, j.Settings, credential, j.Message)
		// time.Now carries a monotonic component. Capture before any persistence,
		// wall-clock callback or filesystem cost, independent of the test clock.
		if !extension && result.Accepted {
			acceptedAt = time.Now()
		}
		if extension {
			j.ExtensionAccepted = result.Accepted
			if result.Accepted {
				j.ExtensionStatus = "sent"
				j.ExtensionDiagnostic = ""
			} else {
				j.ExtensionStatus = "failed"
				j.ExtensionDiagnostic = diagnostic(result.Diagnostic)
			}
		} else if result.Accepted {
			j.Status = "sent"
			j.Accepted = true
			j.FinishedAt = stamp(r.Now())
			if continuous && j.RingSeconds > 30 {
				j.ExtensionStatus = "pending"
			}
		} else {
			j.Diagnostic = diagnostic(result.Diagnostic)
			if !result.Retryable || j.Attempts >= 5 {
				j.Status = "failed"
				j.FinishedAt = stamp(r.Now())
			}
			// Between retries the durable status stays sending: a crash is
			// ambiguous, not a license for another worker to restart delivery.
		}
		if persistenceErr = save(); persistenceErr != nil {
			return worker.Result{}
		}
		return worker.Result{Accepted: result.Accepted, Retryable: result.Retryable}
	}
	sleep := func(deliveryContext context.Context, delay time.Duration) error {
		if persistenceErr != nil {
			return persistenceErr
		}
		if !acceptedAt.IsZero() {
			delay = max(0, delay-time.Since(acceptedAt))
		}
		if r.Sleep != nil {
			return r.Sleep(deliveryContext, delay)
		}
		return sleepContext(deliveryContext, delay)
	}
	report := worker.Deliver(ctx, j.RingSeconds, continuous, attempt, sleep)
	if persistenceErr != nil {
		return persistenceErr
	}
	if report.Accepted {
		if j.ExtensionStatus == "pending" {
			j.ExtensionStatus = "ambiguous"
			j.ExtensionDiagnostic = "ambiguous"
			if err := save(); err != nil {
				return err
			}
		}
		return nil
	}
	if j.Status == "sending" {
		j.Status = "failed"
		j.FinishedAt = stamp(r.Now())
		if err := save(); err != nil {
			return err
		}
	}
	return errDelivery
}
