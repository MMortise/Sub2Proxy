package config

import (
	"fmt"
	"os"
	"sync"
	"time"

	"gopkg.in/yaml.v3"

	"github.com/wuxi/sub2proxy/internal/fsutil"
)

// DebounceInterval collapses bursts of mutations into one disk write (design D6).
const DebounceInterval = time.Second

// Store persists config snapshots to disk atomically, with debounced writes.
// The snapshot func returns the current config to serialize, so the Store never
// holds a stale copy; the orchestrator mutates its in-memory Config under its own
// lock and calls Schedule() afterward.
type Store struct {
	path     string
	snapshot func() *Config
	debounce time.Duration

	mu    sync.Mutex
	timer *time.Timer
}

// NewStore creates a Store writing to path, pulling data via snapshot.
func NewStore(path string, snapshot func() *Config) *Store {
	return &Store{path: path, snapshot: snapshot, debounce: DebounceInterval}
}

// Schedule requests a debounced write. Calls within DebounceInterval collapse
// into a single disk write carrying the latest snapshot.
func (s *Store) Schedule() {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.timer != nil {
		s.timer.Reset(s.debounce)
		return
	}
	s.timer = time.AfterFunc(s.debounce, func() {
		if err := s.writeNow(); err != nil {
			log("config write failed: " + err.Error())
		}
	})
}

// Flush cancels any pending debounced write and persists immediately. Used on
// shutdown (SIGTERM) so no mutation is lost.
func (s *Store) Flush() error {
	s.mu.Lock()
	if s.timer != nil {
		s.timer.Stop()
		s.timer = nil
	}
	s.mu.Unlock()
	return s.writeNow()
}

// writeNow serializes the current snapshot and atomically replaces the file.
func (s *Store) writeNow() error {
	s.mu.Lock()
	s.timer = nil
	s.mu.Unlock()

	cfg := s.snapshot()
	data, err := yaml.Marshal(cfg)
	if err != nil {
		return fmt.Errorf("marshal config: %w", err)
	}
	return fsutil.AtomicWrite(s.path, data, FileMode)
}

// log is overridable for tests; defaults to stderr via the package logger.
var log = func(msg string) { fmt.Fprintln(os.Stderr, "[config] "+msg) }
