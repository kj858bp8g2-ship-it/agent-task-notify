package worker

import (
	"context"
	"reflect"
	"testing"
	"time"
)

func TestDeliveryBounds(t *testing.T) {
	cases := []struct {
		name       string
		ring       int
		continuous bool
		results    []Result
		calls      []bool
		waits      []time.Duration
		report     Report
	}{
		{"retry limit", 30, false, []Result{{Retryable: true}}, []bool{false, false, false, false, false}, []time.Duration{5 * time.Second, 15 * time.Second, 30 * time.Second, 60 * time.Second}, Report{MainAttempts: 5}},
		{"permanent", 30, false, []Result{{}}, []bool{false}, nil, Report{MainAttempts: 1}},
		{"retry success", 30, false, []Result{{Retryable: true}, {Accepted: true}}, []bool{false, false}, []time.Duration{5 * time.Second}, Report{MainAttempts: 2, Accepted: true}},
		{"ring45", 45, true, []Result{{Accepted: true}}, []bool{false, true}, []time.Duration{15 * time.Second}, Report{MainAttempts: 1, Accepted: true, ExtensionAttempted: true, ExtensionAccepted: true}},
		{"ring60", 60, true, []Result{{Accepted: true}}, []bool{false, true}, []time.Duration{30 * time.Second}, Report{MainAttempts: 1, Accepted: true, ExtensionAttempted: true, ExtensionAccepted: true}},
		{"ring30", 30, true, []Result{{Accepted: true}}, []bool{false}, nil, Report{MainAttempts: 1, Accepted: true}},
		{"not continuous", 60, false, []Result{{Accepted: true}}, []bool{false}, nil, Report{MainAttempts: 1, Accepted: true}},
		{"extension failure", 31, true, []Result{{Accepted: true}, {Retryable: true}}, []bool{false, true}, []time.Duration{time.Second}, Report{MainAttempts: 1, Accepted: true, ExtensionAttempted: true}},
		{"too short", 29, true, nil, nil, nil, Report{}}, {"too long", 61, true, nil, nil, nil, Report{}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			var calls []bool
			var waits []time.Duration
			report := Deliver(context.Background(), tc.ring, tc.continuous, func(_ context.Context, ext bool) Result {
				calls = append(calls, ext)
				i := len(calls) - 1
				if i >= len(tc.results) {
					i = len(tc.results) - 1
				}
				return tc.results[i]
			}, func(_ context.Context, d time.Duration) error { waits = append(waits, d); return nil })
			if !reflect.DeepEqual(calls, tc.calls) || !reflect.DeepEqual(waits, tc.waits) || report != tc.report {
				t.Fatalf("calls=%v waits=%v report=%+v", calls, waits, report)
			}
		})
	}
}

func TestDeliveryCancellation(t *testing.T) {
	for _, when := range []string{"before", "send", "sleep"} {
		t.Run(when, func(t *testing.T) {
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			calls := 0
			if when == "before" {
				cancel()
			}
			report := Deliver(ctx, 45, true, func(context.Context, bool) Result {
				calls++
				if when == "send" {
					cancel()
				}
				return Result{Retryable: true}
			}, func(context.Context, time.Duration) error { cancel(); return nil })
			want := 1
			if when == "before" {
				want = 0
			}
			if calls != want || report.MainAttempts != want {
				t.Fatal("send after cancellation")
			}
		})
	}
	if got := Deliver(context.Background(), 30, false, nil, nil); got != (Report{}) {
		t.Fatal("nil send accepted")
	}
}

func TestDeliveryExtensionCancellationAndSleepFailure(t *testing.T) {
	for _, when := range []string{"accepted", "extension wait", "failed wait"} {
		t.Run(when, func(t *testing.T) {
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			calls := 0
			report := Deliver(ctx, 45, true, func(context.Context, bool) Result {
				calls++
				if when == "accepted" {
					cancel()
				}
				return Result{Accepted: true}
			}, func(context.Context, time.Duration) error {
				if when == "extension wait" {
					cancel()
					return nil
				}
				return context.Canceled
			})
			if calls != 1 || !report.Accepted || report.ExtensionAttempted {
				t.Fatal("extension sent after cancellation or failed wait")
			}
		})
	}
}
