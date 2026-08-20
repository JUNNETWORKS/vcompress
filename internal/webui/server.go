package webui

import (
	"context"
	"embed"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"io/fs"
	"net"
	"net/http"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"sort"
	"strings"
	"time"

	"vcompress/internal/config"
)

//go:embed static/*
var staticFiles embed.FS

type ServeOptions struct {
	Port        int
	NoOpen      bool
	Output      io.Writer
	OpenBrowser func(string) error
}

func Handler(manager *Manager) http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("/api/defaults", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			methodNotAllowed(w, http.MethodGet)
			return
		}
		writeJSON(w, http.StatusOK, config.Default())
	})
	mux.HandleFunc("/api/state", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			methodNotAllowed(w, http.MethodGet)
			return
		}
		writeJSON(w, http.StatusOK, manager.Snapshot())
	})
	mux.HandleFunc("/api/jobs", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			methodNotAllowed(w, http.MethodPost)
			return
		}
		if !allowJSONWrite(w, r) {
			return
		}
		cfg := config.Default()
		decoder := json.NewDecoder(http.MaxBytesReader(w, r.Body, 1<<20))
		decoder.DisallowUnknownFields()
		if err := decoder.Decode(&cfg); err != nil {
			writeError(w, http.StatusBadRequest, "invalid job configuration: "+err.Error())
			return
		}
		if err := manager.Start(cfg); err != nil {
			if errors.Is(err, ErrBusy) {
				writeError(w, http.StatusConflict, err.Error())
				return
			}
			writeError(w, http.StatusBadRequest, err.Error())
			return
		}
		writeJSON(w, http.StatusAccepted, manager.Snapshot())
	})
	mux.HandleFunc("/api/jobs/stop", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			methodNotAllowed(w, http.MethodPost)
			return
		}
		if !allowJSONWrite(w, r) {
			return
		}
		var request struct {
			Mode string `json:"mode"`
		}
		decoder := json.NewDecoder(http.MaxBytesReader(w, r.Body, 4096))
		decoder.DisallowUnknownFields()
		if err := decoder.Decode(&request); err != nil {
			writeError(w, http.StatusBadRequest, "invalid stop request: "+err.Error())
			return
		}
		if err := manager.Stop(request.Mode); err != nil {
			if errors.Is(err, ErrNotRunning) {
				writeError(w, http.StatusConflict, err.Error())
				return
			}
			writeError(w, http.StatusBadRequest, err.Error())
			return
		}
		writeJSON(w, http.StatusAccepted, manager.Snapshot())
	})
	mux.HandleFunc("/api/directories", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			methodNotAllowed(w, http.MethodGet)
			return
		}
		listing, err := listDirectories(r.URL.Query().Get("path"))
		if err != nil {
			writeError(w, http.StatusBadRequest, err.Error())
			return
		}
		writeJSON(w, http.StatusOK, listing)
	})
	mux.HandleFunc("/api/events", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			methodNotAllowed(w, http.MethodGet)
			return
		}
		serveEvents(w, r, manager)
	})

	assets, err := fs.Sub(staticFiles, "static")
	if err != nil {
		panic(err)
	}
	fileServer := http.FileServer(http.FS(assets))
	mux.Handle("/", http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Cache-Control", "no-cache")
		fileServer.ServeHTTP(w, r)
	}))
	return mux
}

func Serve(ctx context.Context, opts ServeOptions) error {
	if opts.Port < 1 || opts.Port > 65535 {
		return fmt.Errorf("port must be between 1 and 65535")
	}
	if opts.Output == nil {
		opts.Output = os.Stdout
	}
	manager := NewManager(ctx, nil)
	server := &http.Server{
		Addr:              fmt.Sprintf("127.0.0.1:%d", opts.Port),
		Handler:           Handler(manager),
		ReadHeaderTimeout: 5 * time.Second,
	}
	listener, err := net.Listen("tcp", server.Addr)
	if err != nil {
		return err
	}
	address := "http://" + listener.Addr().String()
	fmt.Fprintf(opts.Output, "vcompress WebUI: %s\n", address)

	openBrowser := opts.OpenBrowser
	if openBrowser == nil {
		openBrowser = OpenBrowser
	}
	if !opts.NoOpen {
		go func() {
			if err := openBrowser(address); err != nil {
				fmt.Fprintf(opts.Output, "warning: could not open browser: %v\n", err)
			}
		}()
	}

	shutdownDone := make(chan struct{})
	go func() {
		select {
		case <-ctx.Done():
			shutdownCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
			defer cancel()
			_ = server.Shutdown(shutdownCtx)
		case <-shutdownDone:
		}
	}()
	err = server.Serve(listener)
	close(shutdownDone)
	if errors.Is(err, http.ErrServerClosed) {
		return nil
	}
	return err
}

func OpenBrowser(address string) error {
	name, args := browserCommand(runtime.GOOS, address)
	return exec.Command(name, args...).Start()
}

func browserCommand(goos, address string) (string, []string) {
	switch goos {
	case "darwin":
		return "open", []string{address}
	case "windows":
		return "rundll32", []string{"url.dll,FileProtocolHandler", address}
	default:
		return "xdg-open", []string{address}
	}
}

type directoryEntry struct {
	Name string `json:"name"`
	Path string `json:"path"`
}

type directoryListing struct {
	Path    string           `json:"path"`
	Parent  string           `json:"parent,omitempty"`
	Entries []directoryEntry `json:"entries"`
}

func listDirectories(path string) (directoryListing, error) {
	if path == "" {
		cwd, err := os.Getwd()
		if err != nil {
			return directoryListing{}, err
		}
		volume := filepath.VolumeName(cwd)
		path = volume + string(filepath.Separator)
	}
	abs, err := filepath.Abs(path)
	if err != nil {
		return directoryListing{}, fmt.Errorf("resolve directory: %w", err)
	}
	abs = filepath.Clean(abs)
	info, err := os.Stat(abs)
	if err != nil {
		return directoryListing{}, fmt.Errorf("inspect directory: %w", err)
	}
	if !info.IsDir() {
		return directoryListing{}, fmt.Errorf("not a directory: %s", abs)
	}
	entries, err := os.ReadDir(abs)
	if err != nil {
		return directoryListing{}, fmt.Errorf("read directory: %w", err)
	}
	listing := directoryListing{Path: abs, Entries: []directoryEntry{}}
	if parent := filepath.Dir(abs); parent != abs {
		listing.Parent = parent
	}
	for _, entry := range entries {
		entryPath := filepath.Join(abs, entry.Name())
		isDir := entry.IsDir()
		if !isDir && entry.Type()&os.ModeSymlink != 0 {
			if target, err := os.Stat(entryPath); err == nil {
				isDir = target.IsDir()
			}
		}
		if isDir {
			listing.Entries = append(listing.Entries, directoryEntry{Name: entry.Name(), Path: entryPath})
		}
	}
	sort.Slice(listing.Entries, func(i, j int) bool {
		return strings.ToLower(listing.Entries[i].Name) < strings.ToLower(listing.Entries[j].Name)
	})
	return listing, nil
}

func serveEvents(w http.ResponseWriter, r *http.Request, manager *Manager) {
	flusher, ok := w.(http.Flusher)
	if !ok {
		writeError(w, http.StatusInternalServerError, "streaming is not supported")
		return
	}
	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")
	updates, unsubscribe := manager.Subscribe()
	defer unsubscribe()
	heartbeat := time.NewTicker(15 * time.Second)
	defer heartbeat.Stop()
	for {
		select {
		case snapshot := <-updates:
			data, err := json.Marshal(snapshot)
			if err != nil {
				return
			}
			fmt.Fprintf(w, "data: %s\n\n", data)
			flusher.Flush()
		case <-heartbeat.C:
			fmt.Fprint(w, ": keepalive\n\n")
			flusher.Flush()
		case <-r.Context().Done():
			return
		}
	}
}

func allowJSONWrite(w http.ResponseWriter, r *http.Request) bool {
	if !strings.HasPrefix(strings.ToLower(r.Header.Get("Content-Type")), "application/json") {
		writeError(w, http.StatusUnsupportedMediaType, "Content-Type must be application/json")
		return false
	}
	origin := r.Header.Get("Origin")
	if origin == "" {
		return true
	}
	parsed, err := url.Parse(origin)
	if err != nil || !strings.EqualFold(parsed.Host, r.Host) {
		writeError(w, http.StatusForbidden, "cross-origin writes are not allowed")
		return false
	}
	return true
}

func methodNotAllowed(w http.ResponseWriter, allow string) {
	w.Header().Set("Allow", allow)
	writeError(w, http.StatusMethodNotAllowed, "method not allowed")
}

func writeError(w http.ResponseWriter, status int, message string) {
	writeJSON(w, status, map[string]string{"error": message})
}

func writeJSON(w http.ResponseWriter, status int, value any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(value)
}

func ParseWebFlags(args []string) (ServeOptions, error) {
	flags := flag.NewFlagSet("vcompress web", flag.ContinueOnError)
	port := flags.Int("port", 8080, "localhost port for the WebUI")
	noOpen := flags.Bool("no-open", false, "do not open the default browser")
	if err := flags.Parse(args); err != nil {
		return ServeOptions{}, err
	}
	if flags.NArg() != 0 {
		return ServeOptions{}, fmt.Errorf("vcompress web does not accept positional arguments")
	}
	return ServeOptions{Port: *port, NoOpen: *noOpen}, nil
}
