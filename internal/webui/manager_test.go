package webui

import (
	"context"
	"errors"
	"testing"
	"time"

	"vcompress/internal/config"
	"vcompress/internal/job"
	"vcompress/internal/processor"
	"vcompress/internal/progress"
)

func TestManagerRejectsConcurrentJobAndPublishesResult(t *testing.T) {
	release := make(chan struct{})
	run := func(_ context.Context, _ config.Config, opts job.Options) (job.Summary, error) {
		opts.Reporter.Report(progress.Event{Phase: progress.PhaseDiscovery, Message: "Discovery complete", Total: 1})
		<-release
		opts.Reporter.Report(progress.Event{Phase: progress.PhaseProbe, Path: "source.mp4"})
		opts.OnResult(processor.Result{Status: processor.StatusSkipped, Path: "source.mp4", Reason: "dry run", OriginalBytes: 100})
		return job.Summary{Total: 1, Processed: 1, Skipped: 1, FinishedAt: time.Now()}, nil
	}
	manager := NewManager(context.Background(), run)
	cfg := config.Default()
	cfg.Root = t.TempDir()
	if err := manager.Start(cfg); err != nil {
		t.Fatal(err)
	}
	if err := manager.Start(cfg); !errors.Is(err, ErrBusy) {
		t.Fatalf("second Start() error = %v, want busy", err)
	}
	close(release)
	snapshot := waitForState(t, manager, StateCompleted)
	if snapshot.Summary.Total != 1 || snapshot.Summary.Skipped != 1 || len(snapshot.Results) != 1 {
		t.Fatalf("snapshot = %+v", snapshot)
	}
	if snapshot.Results[0].Reason != "dry run" || snapshot.Results[0].Status != "skipped" {
		t.Fatalf("result = %+v", snapshot.Results[0])
	}
}

func TestManagerPublishesNormalizedTargets(t *testing.T) {
	release := make(chan struct{})
	run := func(_ context.Context, _ config.Config, _ job.Options) (job.Summary, error) {
		<-release
		return job.Summary{FinishedAt: time.Now()}, nil
	}
	root := t.TempDir()
	manager := NewManager(context.Background(), run)
	cfg := config.Default()
	cfg.Targets = []string{root, root}
	if err := manager.Start(cfg); err != nil {
		t.Fatal(err)
	}
	snapshot := manager.Snapshot()
	if len(snapshot.Targets) != 1 || snapshot.Targets[0] != root {
		t.Fatalf("targets = %v, want [%s]", snapshot.Targets, root)
	}
	close(release)
	waitForState(t, manager, StateCompleted)
}

func TestManagerStopsImmediately(t *testing.T) {
	run := func(ctx context.Context, _ config.Config, _ job.Options) (job.Summary, error) {
		<-ctx.Done()
		return job.Summary{Interrupted: true, FinishedAt: time.Now()}, ctx.Err()
	}
	manager := NewManager(context.Background(), run)
	cfg := config.Default()
	cfg.Root = t.TempDir()
	if err := manager.Start(cfg); err != nil {
		t.Fatal(err)
	}
	if err := manager.Stop("now"); err != nil {
		t.Fatal(err)
	}
	if got := waitForState(t, manager, StateCancelled); !got.Summary.Interrupted {
		t.Fatalf("snapshot = %+v", got)
	}
}

func TestManagerStopsAfterCurrent(t *testing.T) {
	run := func(_ context.Context, _ config.Config, opts job.Options) (job.Summary, error) {
		deadline := time.After(2 * time.Second)
		for !opts.StopAfterCurrent() {
			select {
			case <-deadline:
				return job.Summary{}, errors.New("graceful stop was not observed")
			default:
				time.Sleep(time.Millisecond)
			}
		}
		return job.Summary{Stopped: true, FinishedAt: time.Now()}, nil
	}
	manager := NewManager(context.Background(), run)
	cfg := config.Default()
	cfg.Root = t.TempDir()
	if err := manager.Start(cfg); err != nil {
		t.Fatal(err)
	}
	if err := manager.Stop("after-current"); err != nil {
		t.Fatal(err)
	}
	if got := waitForState(t, manager, StateStopped); !got.Summary.Stopped {
		t.Fatalf("snapshot = %+v", got)
	}
}

func TestManagerBoundsBrowserLogs(t *testing.T) {
	manager := NewManager(context.Background(), nil)
	for i := 0; i < 510; i++ {
		if _, err := manager.Write([]byte("line\n")); err != nil {
			t.Fatal(err)
		}
	}
	if got := len(manager.Snapshot().Logs); got != 500 {
		t.Fatalf("log count = %d, want 500", got)
	}
}

func waitForState(t *testing.T, manager *Manager, want State) Snapshot {
	t.Helper()
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		snapshot := manager.Snapshot()
		if snapshot.State == want {
			return snapshot
		}
		time.Sleep(time.Millisecond)
	}
	t.Fatalf("state = %s, want %s", manager.Snapshot().State, want)
	return Snapshot{}
}
