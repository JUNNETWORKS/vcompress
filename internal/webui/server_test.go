package webui

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"vcompress/internal/config"
	"vcompress/internal/job"
)

func TestDefaultsKeepFullDecodeSafetyEnabled(t *testing.T) {
	request := httptest.NewRequest(http.MethodGet, "/api/defaults", nil)
	response := httptest.NewRecorder()
	Handler(NewManager(context.Background(), nil)).ServeHTTP(response, request)
	if response.Code != http.StatusOK {
		t.Fatalf("status = %d", response.Code)
	}
	var cfg config.Config
	if err := json.NewDecoder(response.Body).Decode(&cfg); err != nil {
		t.Fatal(err)
	}
	if !cfg.FullDecodeCheck {
		t.Fatal("full decode check is disabled in Web defaults")
	}
}

func TestEmbeddedIndexIsServed(t *testing.T) {
	request := httptest.NewRequest(http.MethodGet, "/", nil)
	response := httptest.NewRecorder()
	Handler(NewManager(context.Background(), nil)).ServeHTTP(response, request)
	if response.Code != http.StatusOK || !strings.Contains(response.Body.String(), "vcompress") {
		t.Fatalf("status = %d, body = %s", response.Code, response.Body.String())
	}
}

func TestJobAPIRejectsSecondActiveJob(t *testing.T) {
	release := make(chan struct{})
	run := func(_ context.Context, _ config.Config, _ job.Options) (job.Summary, error) {
		<-release
		return job.Summary{FinishedAt: time.Now()}, nil
	}
	manager := NewManager(context.Background(), run)
	handler := Handler(manager)
	cfg := config.Default()
	cfg.Root = t.TempDir()
	body, err := json.Marshal(cfg)
	if err != nil {
		t.Fatal(err)
	}

	first := httptest.NewRecorder()
	handler.ServeHTTP(first, jsonRequest(http.MethodPost, "/api/jobs", body))
	if first.Code != http.StatusAccepted {
		t.Fatalf("first status = %d, body = %s", first.Code, first.Body.String())
	}
	second := httptest.NewRecorder()
	handler.ServeHTTP(second, jsonRequest(http.MethodPost, "/api/jobs", body))
	if second.Code != http.StatusConflict {
		t.Fatalf("second status = %d, body = %s", second.Code, second.Body.String())
	}
	state := httptest.NewRecorder()
	handler.ServeHTTP(state, httptest.NewRequest(http.MethodGet, "/api/state", nil))
	if !strings.Contains(state.Body.String(), `"state":"running"`) {
		t.Fatalf("state body = %s", state.Body.String())
	}
	close(release)
	waitForState(t, manager, StateCompleted)
}

func TestJobAPIAcceptsFileAndDirectoryTargets(t *testing.T) {
	received := make(chan config.Config, 1)
	run := func(_ context.Context, cfg config.Config, _ job.Options) (job.Summary, error) {
		received <- cfg
		return job.Summary{FinishedAt: time.Now()}, nil
	}
	root := t.TempDir()
	file := filepath.Join(root, "movie.mp4")
	if err := os.WriteFile(file, []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	cfg := config.Default()
	cfg.Targets = []string{root, file}
	body, err := json.Marshal(cfg)
	if err != nil {
		t.Fatal(err)
	}
	response := httptest.NewRecorder()
	Handler(NewManager(context.Background(), run)).ServeHTTP(response, jsonRequest(http.MethodPost, "/api/jobs", body))
	if response.Code != http.StatusAccepted {
		t.Fatalf("status = %d, body = %s", response.Code, response.Body.String())
	}
	got := <-received
	if len(got.Targets) != 2 || got.Targets[0] != root || got.Targets[1] != file {
		t.Fatalf("targets = %v, want [%s %s]", got.Targets, root, file)
	}
}

func TestWriteAPIRequiresJSONAndSameOrigin(t *testing.T) {
	manager := NewManager(context.Background(), nil)
	handler := Handler(manager)
	plain := httptest.NewRequest(http.MethodPost, "/api/jobs", strings.NewReader("{}"))
	plain.Header.Set("Content-Type", "text/plain")
	plainResponse := httptest.NewRecorder()
	handler.ServeHTTP(plainResponse, plain)
	if plainResponse.Code != http.StatusUnsupportedMediaType {
		t.Fatalf("plain status = %d", plainResponse.Code)
	}

	cross := jsonRequest(http.MethodPost, "/api/jobs", []byte("{}"))
	cross.Host = "127.0.0.1:8080"
	cross.Header.Set("Origin", "http://evil.example")
	crossResponse := httptest.NewRecorder()
	handler.ServeHTTP(crossResponse, cross)
	if crossResponse.Code != http.StatusForbidden {
		t.Fatalf("cross-origin status = %d", crossResponse.Code)
	}
}

func TestDirectoryListingReturnsDirectoriesAndVideoFiles(t *testing.T) {
	root := t.TempDir()
	if err := os.Mkdir(filepath.Join(root, "folder"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "movie.mp4"), []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "notes.txt"), []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	request := httptest.NewRequest(http.MethodGet, "/api/directories?path="+root, nil)
	response := httptest.NewRecorder()
	Handler(NewManager(context.Background(), nil)).ServeHTTP(response, request)
	if response.Code != http.StatusOK {
		t.Fatalf("status = %d, body = %s", response.Code, response.Body.String())
	}
	body := response.Body.String()
	if !strings.Contains(body, `"name":"folder"`) || !strings.Contains(body, `"kind":"directory"`) {
		t.Fatalf("listing = %s", body)
	}
	if !strings.Contains(body, `"name":"movie.mp4"`) || !strings.Contains(body, `"kind":"file"`) {
		t.Fatalf("listing = %s", body)
	}
	if strings.Contains(body, "notes.txt") {
		t.Fatalf("listing = %s", response.Body.String())
	}
}

func TestBrowserCommands(t *testing.T) {
	tests := []struct {
		goos string
		name string
		arg  string
	}{
		{goos: "darwin", name: "open", arg: "http://127.0.0.1:8080"},
		{goos: "windows", name: "rundll32", arg: "url.dll,FileProtocolHandler"},
		{goos: "linux", name: "xdg-open", arg: "http://127.0.0.1:8080"},
	}
	for _, test := range tests {
		name, args := browserCommand(test.goos, "http://127.0.0.1:8080")
		if name != test.name || !strings.Contains(strings.Join(args, " "), test.arg) {
			t.Fatalf("browserCommand(%s) = %s %v", test.goos, name, args)
		}
	}
}

func TestParseWebFlags(t *testing.T) {
	opts, err := ParseWebFlags([]string{"--port", "18080", "--no-open"})
	if err != nil {
		t.Fatal(err)
	}
	if opts.Port != 18080 || !opts.NoOpen {
		t.Fatalf("options = %+v", opts)
	}
	if _, err := ParseWebFlags([]string{"unexpected"}); err == nil {
		t.Fatal("positional argument was accepted")
	}
}

func jsonRequest(method, target string, body []byte) *http.Request {
	request := httptest.NewRequest(method, target, bytes.NewReader(body))
	request.Header.Set("Content-Type", "application/json")
	return request
}
