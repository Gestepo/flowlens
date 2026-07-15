package npm

import (
	"bufio"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"syscall"
	"time"

	"flowlens/internal/model"
)

type Cursor struct {
	Device uint64 `json:"device"`
	Inode  uint64 `json:"inode"`
	Offset int64  `json:"offset"`
}

type PollResult struct {
	Requests  []ParsedRequest
	Malformed int
	Truncated bool
}

type Tailer struct {
	cursorPath string
	allowed    []string
	startAtEnd bool
	loaded     bool
	cursors    map[string]Cursor
}

type RecoveryTracker struct {
	reportedHealthy bool
	needsRecovery   bool
}

func (tracker *RecoveryTracker) Observe(result PollResult, pollErr error, observedAt time.Time) *model.Event {
	if pollErr != nil || result.Malformed > 0 || result.Truncated {
		tracker.needsRecovery = true
		return nil
	}
	if tracker.reportedHealthy && !tracker.needsRecovery {
		return nil
	}
	tracker.reportedHealthy = true
	tracker.needsRecovery = false
	event := CollectorHealthyEvent(observedAt)
	return &event
}

func Events(result PollResult, observedAt time.Time) []model.Event {
	events := make([]model.Event, 0, len(result.Requests)+1)
	for _, parsed := range result.Requests {
		request := parsed.Request
		digest := sha256.Sum256([]byte(fmt.Sprintf("%s:%s:%s:%s:%s:%d:%d", parsed.SourceID, parsed.ObservedAt.Format(time.RFC3339Nano), request.Host, request.SourceIP, request.Method, request.Status, request.BytesSent)))
		events = append(events, model.Event{
			ID:           fmt.Sprintf("proxy:%x", digest[:12]),
			ObservedAt:   parsed.ObservedAt.UTC(),
			Kind:         model.EventProxyRequest,
			ProxyRequest: &request,
		})
	}
	if result.Malformed > 0 || result.Truncated {
		code := "malformed_line"
		if result.Truncated {
			code = "log_truncated"
		}
		digest := sha256.Sum256([]byte(fmt.Sprintf("npm-health:%d:%s", observedAt.UnixNano(), code)))
		events = append(events, model.Event{
			ID:         fmt.Sprintf("health:%x", digest[:12]),
			ObservedAt: observedAt.UTC(),
			Kind:       model.EventHealth,
			Health: &model.HealthEvent{
				Collector:     "npm_logs",
				Status:        "degraded",
				Code:          code,
				DroppedEvents: int64(result.Malformed),
				Message:       "NPM log input changed or contained malformed records",
			},
		})
	}
	return events
}

func CollectorUnavailableEvent(observedAt time.Time) model.Event {
	digest := sha256.Sum256([]byte(fmt.Sprintf("npm-unavailable:%d", observedAt.UnixNano())))
	return model.Event{
		ID:         fmt.Sprintf("health:%x", digest[:12]),
		ObservedAt: observedAt.UTC(),
		Kind:       model.EventHealth,
		Health: &model.HealthEvent{
			Collector: "npm_logs",
			Status:    "unhealthy",
			Code:      "collector_unavailable",
			Message:   "NPM log collector could not read configured input",
		},
	}
}

func CollectorHealthyEvent(observedAt time.Time) model.Event {
	digest := sha256.Sum256([]byte(fmt.Sprintf("npm-healthy:%d", observedAt.UnixNano())))
	return model.Event{
		ID:         fmt.Sprintf("health:%x", digest[:12]),
		ObservedAt: observedAt.UTC(),
		Kind:       model.EventHealth,
		Health: &model.HealthEvent{
			Collector: "npm_logs",
			Status:    "healthy",
			Code:      "active",
			Message:   "NPM log collector is active",
		},
	}
}

func NewTailer(cursorPath string, allowedRoots []string, startAtEnd bool) *Tailer {
	allowed := make([]string, 0, len(allowedRoots))
	for _, root := range allowedRoots {
		allowed = append(allowed, filepath.Clean(root))
	}
	return &Tailer{cursorPath: cursorPath, allowed: allowed, startAtEnd: startAtEnd, cursors: make(map[string]Cursor)}
}

func (tailer *Tailer) Poll(patterns []string) (PollResult, error) {
	if err := tailer.load(); err != nil {
		return PollResult{}, err
	}
	result := PollResult{}
	for _, pattern := range patterns {
		pattern = filepath.Clean(pattern)
		if !tailer.isAllowed(pattern) {
			return PollResult{}, fmt.Errorf("NPM log glob must be within allowed roots")
		}
		paths, err := filepath.Glob(pattern)
		if err != nil {
			return PollResult{}, fmt.Errorf("expand NPM log glob: %w", err)
		}
		for _, path := range paths {
			if err := tailer.pollPath(path, &result); err != nil {
				return PollResult{}, err
			}
		}
	}
	if err := tailer.persist(); err != nil {
		return PollResult{}, err
	}
	return result, nil
}

func (tailer *Tailer) pollPath(path string, result *PollResult) error {
	info, err := os.Stat(path)
	if err != nil {
		return fmt.Errorf("stat NPM access log: %w", err)
	}
	device, inode, err := fileIdentity(info)
	if err != nil {
		return err
	}
	cursor, found := tailer.cursors[path]
	if !found {
		cursor = Cursor{Device: device, Inode: inode}
		if tailer.startAtEnd {
			cursor.Offset = info.Size()
			tailer.cursors[path] = cursor
			return nil
		}
	}

	if cursor.Device != device || cursor.Inode != inode {
		rotated := findInode(filepath.Dir(path), cursor.Device, cursor.Inode)
		if rotated != "" {
			offset, err := readRequests(rotated, cursor.Offset, result)
			if err != nil {
				return err
			}
			cursor.Offset = offset
		}
		cursor = Cursor{Device: device, Inode: inode, Offset: 0}
	} else if info.Size() < cursor.Offset {
		cursor.Offset = 0
		result.Truncated = true
	}
	offset, err := readRequests(path, cursor.Offset, result)
	if err != nil {
		return err
	}
	cursor.Offset = offset
	tailer.cursors[path] = cursor
	return nil
}

func readRequests(path string, offset int64, result *PollResult) (int64, error) {
	file, err := os.Open(path)
	if err != nil {
		return offset, fmt.Errorf("open NPM access log: %w", err)
	}
	defer file.Close()
	info, err := file.Stat()
	if err != nil {
		return offset, fmt.Errorf("stat open NPM access log: %w", err)
	}
	device, inode, err := fileIdentity(info)
	if err != nil {
		return offset, err
	}
	if _, err := file.Seek(offset, io.SeekStart); err != nil {
		return offset, fmt.Errorf("seek NPM access log: %w", err)
	}
	reader := bufio.NewReader(file)
	for {
		line, err := reader.ReadString('\n')
		if len(line) > 0 && strings.HasSuffix(line, "\n") {
			offset += int64(len(line))
			parsed, parseErr := ParseLine(strings.TrimSuffix(line, "\n"))
			if parseErr != nil {
				result.Malformed++
			} else {
				parsed.SourceID = fmt.Sprintf("%d:%d:%d", device, inode, offset)
				result.Requests = append(result.Requests, parsed)
			}
		}
		if errors.Is(err, io.EOF) {
			return offset, nil
		}
		if err != nil {
			return offset, fmt.Errorf("read NPM access log: %w", err)
		}
	}
}

func (tailer *Tailer) load() error {
	if tailer.loaded {
		return nil
	}
	tailer.loaded = true
	contents, err := os.ReadFile(tailer.cursorPath)
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}
	if err != nil {
		return fmt.Errorf("read NPM cursors: %w", err)
	}
	if err := json.Unmarshal(contents, &tailer.cursors); err != nil {
		return fmt.Errorf("decode NPM cursors: %w", err)
	}
	return nil
}

func (tailer *Tailer) persist() error {
	directory := filepath.Dir(tailer.cursorPath)
	if err := os.MkdirAll(directory, 0o700); err != nil {
		return fmt.Errorf("create NPM cursor directory: %w", err)
	}
	temporary, err := os.CreateTemp(directory, ".npm-cursors-*.tmp")
	if err != nil {
		return fmt.Errorf("create NPM cursor file: %w", err)
	}
	temporaryPath := temporary.Name()
	defer os.Remove(temporaryPath)
	if err := temporary.Chmod(0o600); err != nil {
		_ = temporary.Close()
		return err
	}
	if err := json.NewEncoder(temporary).Encode(tailer.cursors); err != nil {
		_ = temporary.Close()
		return fmt.Errorf("encode NPM cursors: %w", err)
	}
	if err := temporary.Sync(); err != nil {
		_ = temporary.Close()
		return err
	}
	if err := temporary.Close(); err != nil {
		return err
	}
	if err := os.Rename(temporaryPath, tailer.cursorPath); err != nil {
		return fmt.Errorf("publish NPM cursors: %w", err)
	}
	return nil
}

func (tailer *Tailer) isAllowed(path string) bool {
	for _, root := range tailer.allowed {
		relative, err := filepath.Rel(root, path)
		if err == nil && relative != ".." && !strings.HasPrefix(relative, ".."+string(filepath.Separator)) {
			return true
		}
	}
	return false
}

func fileIdentity(info os.FileInfo) (uint64, uint64, error) {
	stat, ok := info.Sys().(*syscall.Stat_t)
	if !ok {
		return 0, 0, fmt.Errorf("NPM log stat has unexpected type")
	}
	return uint64(stat.Dev), stat.Ino, nil
}

func findInode(directory string, device, inode uint64) string {
	entries, err := os.ReadDir(directory)
	if err != nil {
		return ""
	}
	for _, entry := range entries {
		if entry.IsDir() {
			continue
		}
		info, err := entry.Info()
		if err != nil {
			continue
		}
		entryDevice, entryInode, err := fileIdentity(info)
		if err == nil && entryDevice == device && entryInode == inode {
			return filepath.Join(directory, entry.Name())
		}
	}
	return ""
}
