package spool

import (
	"compress/gzip"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"math"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"flowlens/internal/model"
)

const (
	defaultMaxBytes = int64(256 << 20)
	defaultMaxAge   = 30 * time.Minute
)

type Option func(*Spool)

type Spool struct {
	dir      string
	maxBytes int64
	maxAge   time.Duration
	now      func() time.Time
}

type Item struct {
	Batch model.Batch
	path  string
}

type DropReport struct {
	Code           string
	DroppedBatches int64
	DroppedEvents  int64
	UsagePercent   float64
}

func WithClock(clock func() time.Time) Option {
	return func(s *Spool) { s.now = clock }
}

func WithMaxBytes(maxBytes int64) Option {
	return func(s *Spool) { s.maxBytes = maxBytes }
}

func WithMaxAge(maxAge time.Duration) Option {
	return func(s *Spool) { s.maxAge = maxAge }
}

func New(dir string, options ...Option) (*Spool, error) {
	queue := &Spool{dir: dir, maxBytes: defaultMaxBytes, maxAge: defaultMaxAge, now: time.Now}
	for _, option := range options {
		option(queue)
	}
	if dir == "" || queue.maxBytes <= 0 || queue.maxAge <= 0 || queue.now == nil {
		return nil, errors.New("spool requires a directory, positive limits, and a clock")
	}
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return nil, fmt.Errorf("create spool directory: %w", err)
	}
	return queue, nil
}

func (s *Spool) Enqueue(batch model.Batch) (DropReport, error) {
	if err := batch.Validate(); err != nil {
		return DropReport{}, fmt.Errorf("validate queued batch: %w", err)
	}
	queuedAt := s.now().UTC()
	name := batchFileName(batch.BatchID, queuedAt)
	finalPath := filepath.Join(s.dir, name)
	temporary, err := os.CreateTemp(s.dir, ".flowlens-*.tmp")
	if err != nil {
		return DropReport{}, fmt.Errorf("create spool temporary file: %w", err)
	}
	temporaryPath := temporary.Name()
	keepTemporary := true
	defer func() {
		if keepTemporary {
			_ = os.Remove(temporaryPath)
		}
	}()
	if err := temporary.Chmod(0o600); err != nil {
		_ = temporary.Close()
		return DropReport{}, fmt.Errorf("secure spool temporary file: %w", err)
	}

	compressed := gzip.NewWriter(temporary)
	if err := json.NewEncoder(compressed).Encode(batch); err != nil {
		_ = compressed.Close()
		_ = temporary.Close()
		return DropReport{}, fmt.Errorf("encode queued batch: %w", err)
	}
	if err := compressed.Close(); err != nil {
		_ = temporary.Close()
		return DropReport{}, fmt.Errorf("close queued gzip stream: %w", err)
	}
	if err := temporary.Sync(); err != nil {
		_ = temporary.Close()
		return DropReport{}, fmt.Errorf("sync queued batch: %w", err)
	}
	if err := temporary.Close(); err != nil {
		return DropReport{}, fmt.Errorf("close queued batch: %w", err)
	}
	if err := os.Rename(temporaryPath, finalPath); err != nil {
		return DropReport{}, fmt.Errorf("publish queued batch: %w", err)
	}
	keepTemporary = false
	if err := os.Chtimes(finalPath, queuedAt, queuedAt); err != nil {
		return DropReport{}, fmt.Errorf("set queued batch time: %w", err)
	}
	if err := syncDirectory(s.dir); err != nil {
		return DropReport{}, err
	}
	return s.prune()
}

func (s *Spool) Peek() (Item, bool, error) {
	files, err := s.files()
	if err != nil {
		return Item{}, false, err
	}
	if len(files) == 0 {
		return Item{}, false, nil
	}
	path := filepath.Join(s.dir, files[0].Name())
	file, err := os.Open(path)
	if err != nil {
		return Item{}, false, fmt.Errorf("open queued batch: %w", err)
	}
	defer file.Close()
	compressed, err := gzip.NewReader(file)
	if err != nil {
		return Item{}, false, fmt.Errorf("open queued gzip stream: %w", err)
	}
	defer compressed.Close()
	var batch model.Batch
	decoder := json.NewDecoder(io.LimitReader(compressed, 16<<20))
	if err := decoder.Decode(&batch); err != nil {
		return Item{}, false, fmt.Errorf("decode queued batch: %w", err)
	}
	if err := batch.Validate(); err != nil {
		return Item{}, false, fmt.Errorf("validate queued batch: %w", err)
	}
	return Item{Batch: batch, path: path}, true, nil
}

func (s *Spool) Ack(item Item) error {
	if filepath.Dir(item.path) != filepath.Clean(s.dir) || !strings.HasSuffix(item.path, ".json.gz") {
		return errors.New("queued item does not belong to this spool")
	}
	if err := os.Remove(item.path); err != nil {
		return fmt.Errorf("remove acknowledged batch: %w", err)
	}
	return syncDirectory(s.dir)
}

func (s *Spool) prune() (DropReport, error) {
	files, err := s.files()
	if err != nil {
		return DropReport{}, err
	}
	if err := s.compactExpiredInterfaces(files); err != nil {
		return DropReport{}, err
	}
	files, err = s.files()
	if err != nil {
		return DropReport{}, err
	}
	var total int64
	for _, file := range files {
		total += file.Size()
	}
	report := DropReport{}
	for len(files) > 1 {
		candidate := -1
		for index, file := range files {
			expired := s.now().UTC().Sub(file.ModTime()) > s.maxAge
			if !expired && total <= s.maxBytes {
				continue
			}
			batch, err := readBatch(filepath.Join(s.dir, file.Name()))
			if err != nil {
				return DropReport{}, err
			}
			if protectedBatch(batch) {
				continue
			}
			candidate = index
			break
		}
		if candidate < 0 {
			break
		}
		dropped := files[candidate]
		path := filepath.Join(s.dir, dropped.Name())
		batch, err := readBatch(path)
		if err != nil {
			return DropReport{}, err
		}
		if err := os.Remove(path); err != nil {
			return DropReport{}, fmt.Errorf("evict queued batch: %w", err)
		}
		report.Code = "data_gap"
		report.DroppedBatches++
		report.DroppedEvents += int64(len(batch.Events))
		total -= dropped.Size()
		files = append(files[:candidate], files[candidate+1:]...)
	}
	if report.DroppedBatches > 0 {
		if err := syncDirectory(s.dir); err != nil {
			return DropReport{}, err
		}
	}
	if len(files) > 0 {
		byteUsage := float64(total) / float64(s.maxBytes)
		age := s.now().UTC().Sub(files[0].ModTime())
		if age < 0 {
			age = 0
		}
		ageUsage := float64(age) / float64(s.maxAge)
		report.UsagePercent = math.Min(100, math.Max(byteUsage, ageUsage)*100)
	}
	return report, nil
}

func (s *Spool) compactExpiredInterfaces(files []os.FileInfo) error {
	type summaryKey struct {
		node   string
		minute time.Time
	}
	summaries := make(map[summaryKey]model.Batch)
	var remove []string
	for _, file := range files {
		path := filepath.Join(s.dir, file.Name())
		batch, err := readBatch(path)
		if err != nil {
			return err
		}
		if strings.HasPrefix(batch.BatchID, "minute-summary:") {
			if len(batch.Events) > 0 {
				summaries[summaryKey{node: batch.NodeID, minute: batch.Events[0].ObservedAt.UTC().Truncate(time.Minute)}] = batch
			}
			continue
		}
		if s.now().UTC().Sub(file.ModTime()) <= s.maxAge || !interfaceOnly(batch) {
			continue
		}
		eventsByMinute := make(map[summaryKey][]model.Event)
		for _, event := range batch.Events {
			minute := event.ObservedAt.UTC().Truncate(time.Minute)
			key := summaryKey{node: batch.NodeID, minute: minute}
			eventsByMinute[key] = append(eventsByMinute[key], event)
		}
		for key, events := range eventsByMinute {
			summary, ok := summaries[key]
			if !ok {
				summary = model.Batch{SchemaVersion: 1, BatchID: minuteSummaryBatchID(batch.NodeID, key.minute), NodeID: batch.NodeID, SentAt: key.minute, Events: []model.Event{}}
			}
			if containsString(summary.CompactedBatchIDs, batch.BatchID) {
				continue
			}
			for _, event := range events {
				summary.Events = mergeInterfaceEvent(summary.Events, event, key.minute)
			}
			summary.CompactedBatchIDs = append(summary.CompactedBatchIDs, batch.BatchID)
			sort.Strings(summary.CompactedBatchIDs)
			summaries[key] = summary
		}
		remove = append(remove, path)
	}
	if len(remove) == 0 {
		return nil
	}
	for key, summary := range summaries {
		path := filepath.Join(s.dir, batchFileName(summary.BatchID, key.minute))
		if err := writeBatchFile(s.dir, path, summary, key.minute); err != nil {
			return err
		}
	}
	if err := syncDirectory(s.dir); err != nil {
		return err
	}
	for _, path := range remove {
		if err := os.Remove(path); err != nil {
			return fmt.Errorf("remove compacted interface batch: %w", err)
		}
	}
	return syncDirectory(s.dir)
}

func containsString(values []string, wanted string) bool {
	for _, value := range values {
		if value == wanted {
			return true
		}
	}
	return false
}

func interfaceOnly(batch model.Batch) bool {
	if len(batch.Events) == 0 {
		return false
	}
	for _, event := range batch.Events {
		if event.Kind != model.EventInterfaceDelta {
			return false
		}
	}
	return true
}

func protectedBatch(batch model.Batch) bool {
	if strings.HasPrefix(batch.BatchID, "minute-summary:") || interfaceOnly(batch) {
		return true
	}
	for _, event := range batch.Events {
		if event.Kind != model.EventHealth {
			return false
		}
	}
	return len(batch.Events) > 0
}

func mergeInterfaceEvent(events []model.Event, incoming model.Event, minute time.Time) []model.Event {
	for index := range events {
		if events[index].Interface == incoming.Interface && events[index].Direction == incoming.Direction {
			events[index].Bytes = saturatingAdd(events[index].Bytes, incoming.Bytes)
			events[index].Packets = saturatingAdd(events[index].Packets, incoming.Packets)
			return events
		}
	}
	digest := sha256.Sum256([]byte(fmt.Sprintf("%s:%s:%d", incoming.Interface, incoming.Direction, minute.Unix())))
	incoming.ID = fmt.Sprintf("minute-interface:%x", digest[:12])
	incoming.ObservedAt = minute
	return append(events, incoming)
}

func minuteSummaryBatchID(nodeID string, minute time.Time) string {
	digest := sha256.Sum256([]byte(nodeID))
	return fmt.Sprintf("minute-summary:%x:%d", digest[:8], minute.Unix())
}

func batchFileName(batchID string, queuedAt time.Time) string {
	digest := sha256.Sum256([]byte(batchID))
	return fmt.Sprintf("%020d-%x.json.gz", queuedAt.UnixNano(), digest[:8])
}

func writeBatchFile(dir, finalPath string, batch model.Batch, queuedAt time.Time) error {
	if err := batch.Validate(); err != nil {
		return fmt.Errorf("validate compacted batch: %w", err)
	}
	temporary, err := os.CreateTemp(dir, ".flowlens-summary-*.tmp")
	if err != nil {
		return fmt.Errorf("create compacted batch: %w", err)
	}
	temporaryPath := temporary.Name()
	defer os.Remove(temporaryPath)
	if err := temporary.Chmod(0o600); err != nil {
		_ = temporary.Close()
		return err
	}
	compressed := gzip.NewWriter(temporary)
	if err := json.NewEncoder(compressed).Encode(batch); err != nil {
		_ = compressed.Close()
		_ = temporary.Close()
		return fmt.Errorf("encode compacted batch: %w", err)
	}
	if err := compressed.Close(); err != nil {
		_ = temporary.Close()
		return err
	}
	if err := temporary.Sync(); err != nil {
		_ = temporary.Close()
		return err
	}
	if err := temporary.Close(); err != nil {
		return err
	}
	if err := os.Rename(temporaryPath, finalPath); err != nil {
		return fmt.Errorf("publish compacted batch: %w", err)
	}
	if err := os.Chtimes(finalPath, queuedAt, queuedAt); err != nil {
		return fmt.Errorf("set compacted batch time: %w", err)
	}
	return nil
}

func saturatingAdd(left, right int64) int64 {
	if right > 0 && left > math.MaxInt64-right {
		return math.MaxInt64
	}
	return left + right
}

func (s *Spool) files() ([]os.FileInfo, error) {
	entries, err := os.ReadDir(s.dir)
	if err != nil {
		return nil, fmt.Errorf("list spool directory: %w", err)
	}
	files := make([]os.FileInfo, 0, len(entries))
	for _, entry := range entries {
		if entry.IsDir() || !strings.HasSuffix(entry.Name(), ".json.gz") {
			continue
		}
		info, err := entry.Info()
		if err != nil {
			return nil, fmt.Errorf("stat queued batch: %w", err)
		}
		files = append(files, info)
	}
	sort.Slice(files, func(i, j int) bool { return files[i].Name() < files[j].Name() })
	return files, nil
}

func readBatch(path string) (model.Batch, error) {
	file, err := os.Open(path)
	if err != nil {
		return model.Batch{}, fmt.Errorf("open queued batch for eviction: %w", err)
	}
	defer file.Close()
	reader, err := gzip.NewReader(file)
	if err != nil {
		return model.Batch{}, fmt.Errorf("open queued batch for eviction: %w", err)
	}
	defer reader.Close()
	var batch model.Batch
	if err := json.NewDecoder(io.LimitReader(reader, 16<<20)).Decode(&batch); err != nil {
		return model.Batch{}, fmt.Errorf("decode queued batch for eviction: %w", err)
	}
	return batch, nil
}

func syncDirectory(dir string) error {
	file, err := os.Open(dir)
	if err != nil {
		return fmt.Errorf("open spool directory for sync: %w", err)
	}
	defer file.Close()
	if err := file.Sync(); err != nil {
		return fmt.Errorf("sync spool directory: %w", err)
	}
	return nil
}
