package logging

import (
	"fmt"
	"io"
	"os"
	"sync"
	"time"
)

type Logger struct {
	mu sync.Mutex
	w  io.Writer
	f  *os.File
}

func New(logPath string, console io.Writer) (*Logger, error) {
	f, err := os.OpenFile(logPath, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0o644)
	if err != nil {
		return nil, err
	}
	if console == nil {
		console = io.Discard
	}
	return &Logger{w: io.MultiWriter(console, f), f: f}, nil
}

func NewWriter(w io.Writer) *Logger {
	return &Logger{w: w}
}

func (l *Logger) Close() error {
	if l.f != nil {
		return l.f.Close()
	}
	return nil
}

func (l *Logger) Printf(format string, args ...any) {
	l.mu.Lock()
	defer l.mu.Unlock()
	msg := fmt.Sprintf(format, args...)
	fmt.Fprintf(l.w, "[%s] %s\n", time.Now().Format("2006-01-02 15:04:05"), msg)
}
