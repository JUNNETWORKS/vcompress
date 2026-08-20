package webui

import (
	"context"
	"errors"
	"strings"
	"sync"
	"time"

	"vcompress/internal/config"
	"vcompress/internal/job"
	"vcompress/internal/processor"
	"vcompress/internal/progress"
)

type State string

const (
	StateIdle      State = "idle"
	StateRunning   State = "running"
	StateStopping  State = "stopping"
	StateCompleted State = "completed"
	StateStopped   State = "stopped"
	StateCancelled State = "cancelled"
	StateFailed    State = "failed"
)

var (
	ErrBusy       = errors.New("a job is already running")
	ErrNotRunning = errors.New("no job is running")
)

type FileResult struct {
	Status        string `json:"status"`
	Path          string `json:"path"`
	OutputPath    string `json:"output_path,omitempty"`
	Reason        string `json:"reason,omitempty"`
	SavedBytes    int64  `json:"saved_bytes"`
	OriginalBytes int64  `json:"original_bytes"`
	OutputBytes   int64  `json:"output_bytes"`
}

type Snapshot struct {
	Revision        uint64         `json:"revision"`
	State           State          `json:"state"`
	Root            string         `json:"root,omitempty"`
	Current         progress.Event `json:"current"`
	Summary         job.Summary    `json:"summary"`
	BatchETASeconds float64        `json:"batch_eta_seconds,omitempty"`
	Results         []FileResult   `json:"results"`
	Logs            []string       `json:"logs"`
	Error           string         `json:"error,omitempty"`
}

type RunFunc func(context.Context, config.Config, job.Options) (job.Summary, error)

type Manager struct {
	mu               sync.Mutex
	parent           context.Context
	run              RunFunc
	snapshot         Snapshot
	cancel           context.CancelFunc
	stopAfterCurrent bool
	subscribers      map[chan Snapshot]struct{}
	currentStarted   time.Time
	completedSeconds float64
}

func NewManager(parent context.Context, run RunFunc) *Manager {
	if parent == nil {
		parent = context.Background()
	}
	if run == nil {
		run = job.Run
	}
	return &Manager{
		parent: parent, run: run, subscribers: map[chan Snapshot]struct{}{},
		snapshot: Snapshot{State: StateIdle, Results: []FileResult{}, Logs: []string{}},
	}
}

func (m *Manager) Start(cfg config.Config) error {
	if err := cfg.Normalize(); err != nil {
		return err
	}
	m.mu.Lock()
	if m.snapshot.State == StateRunning || m.snapshot.State == StateStopping {
		m.mu.Unlock()
		return ErrBusy
	}
	ctx, cancel := context.WithCancel(m.parent)
	m.cancel = cancel
	m.stopAfterCurrent = false
	m.currentStarted = time.Time{}
	m.completedSeconds = 0
	m.snapshot = Snapshot{
		State: StateRunning, Root: cfg.Root,
		Summary: job.Summary{StartedAt: time.Now()},
		Results: []FileResult{}, Logs: []string{},
	}
	m.publishLocked()
	m.mu.Unlock()

	go func() {
		summary, err := m.run(ctx, cfg, job.Options{
			Console:          m,
			Reporter:         m,
			StopAfterCurrent: m.shouldStopAfterCurrent,
			OnResult:         m.onResult,
		})
		m.finish(summary, err)
	}()
	return nil
}

func (m *Manager) Stop(mode string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.snapshot.State != StateRunning && m.snapshot.State != StateStopping {
		return ErrNotRunning
	}
	switch mode {
	case "now":
		m.snapshot.State = StateStopping
		m.stopAfterCurrent = false
		if m.cancel != nil {
			m.cancel()
		}
	case "after-current":
		m.snapshot.State = StateStopping
		m.stopAfterCurrent = true
	default:
		return errors.New("stop mode must be now or after-current")
	}
	m.publishLocked()
	return nil
}

func (m *Manager) Snapshot() Snapshot {
	m.mu.Lock()
	defer m.mu.Unlock()
	return cloneSnapshot(m.snapshot)
}

func (m *Manager) Report(event progress.Event) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if event.Phase == progress.PhaseProbe && event.Path != "" && event.Path != m.snapshot.Current.Path {
		m.currentStarted = time.Now()
	}
	m.snapshot.Current = event
	if event.Total > 0 || (event.Phase == progress.PhaseDiscovery && event.Message == "Discovery complete") {
		m.snapshot.Summary.Total = event.Total
	}
	m.publishLocked()
}

func (m *Manager) Write(data []byte) (int, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	for _, line := range strings.Split(strings.TrimRight(string(data), "\r\n"), "\n") {
		line = strings.TrimRight(line, "\r")
		if line == "" {
			continue
		}
		m.snapshot.Logs = append(m.snapshot.Logs, line)
	}
	if len(m.snapshot.Logs) > 500 {
		m.snapshot.Logs = append([]string(nil), m.snapshot.Logs[len(m.snapshot.Logs)-500:]...)
	}
	m.publishLocked()
	return len(data), nil
}

func (m *Manager) Subscribe() (<-chan Snapshot, func()) {
	ch := make(chan Snapshot, 1)
	m.mu.Lock()
	m.subscribers[ch] = struct{}{}
	ch <- cloneSnapshot(m.snapshot)
	m.mu.Unlock()
	return ch, func() {
		m.mu.Lock()
		delete(m.subscribers, ch)
		m.mu.Unlock()
	}
}

func (m *Manager) shouldStopAfterCurrent() bool {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.stopAfterCurrent
}

func (m *Manager) onResult(result processor.Result) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.snapshot.Results = append(m.snapshot.Results, FileResult{
		Status: result.Status.String(), Path: result.Path, OutputPath: result.OutputPath,
		Reason: result.Reason, SavedBytes: result.SavedBytes,
		OriginalBytes: result.OriginalBytes, OutputBytes: result.OutputBytes,
	})
	m.snapshot.Summary.Processed++
	switch result.Status {
	case processor.StatusConverted:
		m.snapshot.Summary.Converted++
		m.snapshot.Summary.SavedBytes += result.SavedBytes
	case processor.StatusSkipped:
		m.snapshot.Summary.Skipped++
	case processor.StatusFailed:
		m.snapshot.Summary.Failed++
	}
	if !m.currentStarted.IsZero() {
		m.completedSeconds += time.Since(m.currentStarted).Seconds()
		m.currentStarted = time.Time{}
	}
	remaining := m.snapshot.Summary.Total - m.snapshot.Summary.Processed
	if remaining > 0 && m.snapshot.Summary.Processed > 0 {
		m.snapshot.BatchETASeconds = m.completedSeconds / float64(m.snapshot.Summary.Processed) * float64(remaining)
	} else {
		m.snapshot.BatchETASeconds = 0
	}
	m.publishLocked()
}

func (m *Manager) finish(summary job.Summary, err error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.snapshot.Summary = summary
	m.snapshot.BatchETASeconds = 0
	m.cancel = nil
	if err != nil && !job.IsInterrupted(err) {
		m.snapshot.State = StateFailed
		m.snapshot.Error = err.Error()
	} else if summary.Interrupted {
		m.snapshot.State = StateCancelled
	} else if summary.Stopped {
		m.snapshot.State = StateStopped
	} else {
		m.snapshot.State = StateCompleted
	}
	m.publishLocked()
}

func (m *Manager) publishLocked() {
	m.snapshot.Revision++
	snapshot := cloneSnapshot(m.snapshot)
	for ch := range m.subscribers {
		select {
		case ch <- snapshot:
		default:
			select {
			case <-ch:
			default:
			}
			select {
			case ch <- snapshot:
			default:
			}
		}
	}
}

func cloneSnapshot(snapshot Snapshot) Snapshot {
	snapshot.Results = append([]FileResult{}, snapshot.Results...)
	snapshot.Logs = append([]string{}, snapshot.Logs...)
	return snapshot
}
