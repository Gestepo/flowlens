package process

import (
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"time"
	"unicode"
)

const cacheLifetime = 5 * time.Second

type Process struct {
	PID        uint32
	Name       string
	CgroupPath string
	StartTime  uint64
}

type cacheEntry struct {
	process   Process
	expiresAt time.Time
}

type Lookup struct {
	procRoot string
	now      func() time.Time
	mu       sync.Mutex
	cache    map[uint32]cacheEntry
}

func NewLookup(procRoot string, now func() time.Time) *Lookup {
	if now == nil {
		now = time.Now
	}
	return &Lookup{procRoot: procRoot, now: now, cache: make(map[uint32]cacheEntry)}
}

func (lookup *Lookup) Lookup(pid uint32) (Process, bool) {
	directory := filepath.Join(lookup.procRoot, strconv.FormatUint(uint64(pid), 10))
	startTime, ok := readStartTime(filepath.Join(directory, "stat"))
	if !ok {
		lookup.remove(pid)
		return Process{}, false
	}

	lookup.mu.Lock()
	entry, found := lookup.cache[pid]
	if found && entry.process.StartTime == startTime && lookup.now().Before(entry.expiresAt) {
		lookup.mu.Unlock()
		return entry.process, true
	}
	lookup.mu.Unlock()

	comm, err := os.ReadFile(filepath.Join(directory, "comm"))
	if err != nil {
		lookup.remove(pid)
		return Process{}, false
	}
	cgroup, err := os.ReadFile(filepath.Join(directory, "cgroup"))
	if err != nil {
		lookup.remove(pid)
		return Process{}, false
	}
	result := Process{PID: pid, Name: sanitizeName(string(comm)), CgroupPath: parseCgroup(string(cgroup)), StartTime: startTime}
	if result.Name == "" {
		return Process{}, false
	}
	lookup.mu.Lock()
	lookup.cache[pid] = cacheEntry{process: result, expiresAt: lookup.now().Add(cacheLifetime)}
	lookup.mu.Unlock()
	return result, true
}

func (lookup *Lookup) remove(pid uint32) {
	lookup.mu.Lock()
	delete(lookup.cache, pid)
	lookup.mu.Unlock()
}

func readStartTime(path string) (uint64, bool) {
	contents, err := os.ReadFile(path)
	if err != nil {
		return 0, false
	}
	closing := strings.LastIndexByte(string(contents), ')')
	if closing < 0 {
		return 0, false
	}
	fields := strings.Fields(string(contents[closing+1:]))
	if len(fields) <= 19 {
		return 0, false
	}
	value, err := strconv.ParseUint(fields[19], 10, 64)
	return value, err == nil
}

func parseCgroup(contents string) string {
	for _, line := range strings.Split(contents, "\n") {
		parts := strings.SplitN(line, ":", 3)
		if len(parts) == 3 && parts[2] != "" {
			return parts[2]
		}
	}
	return ""
}

func sanitizeName(value string) string {
	value = strings.TrimSpace(value)
	value = filepath.Base(value)
	value = strings.Map(func(character rune) rune {
		if unicode.IsControl(character) {
			return '?'
		}
		return character
	}, value)
	if len(value) > 128 {
		value = value[:128]
	}
	return value
}
