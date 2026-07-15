package interfacecounter

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"

	"flowlens/internal/model"
)

const checkpointVersion = 1

type Checkpoint struct {
	SchemaVersion int                `json:"schema_version"`
	NodeID        string             `json:"node_id"`
	Counters      map[string]Counter `json:"counters"`
	PendingBatch  *model.Batch       `json:"pending_batch,omitempty"`
}

type StateFile struct{ path string }

func NewStateFile(path string) *StateFile { return &StateFile{path: filepath.Clean(path)} }

func NewCheckpoint(nodeID string, counters map[string]Counter, pending *model.Batch) Checkpoint {
	return Checkpoint{SchemaVersion: checkpointVersion, NodeID: nodeID, Counters: counters, PendingBatch: pending}
}

func (store *StateFile) Load() (Checkpoint, bool, error) {
	file, err := os.Open(store.path)
	if errors.Is(err, os.ErrNotExist) {
		return Checkpoint{}, false, nil
	}
	if err != nil {
		return Checkpoint{}, false, fmt.Errorf("open interface counter checkpoint: %w", err)
	}
	defer file.Close()
	var checkpoint Checkpoint
	decoder := json.NewDecoder(io.LimitReader(file, 1<<20))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&checkpoint); err != nil {
		return Checkpoint{}, false, fmt.Errorf("decode interface counter checkpoint: %w", err)
	}
	if err := validateCheckpoint(checkpoint); err != nil {
		return Checkpoint{}, false, err
	}
	return checkpoint, true, nil
}

func (store *StateFile) Save(checkpoint Checkpoint) error {
	if err := validateCheckpoint(checkpoint); err != nil {
		return err
	}
	directory := filepath.Dir(store.path)
	if err := os.MkdirAll(directory, 0o700); err != nil {
		return fmt.Errorf("create interface counter checkpoint directory: %w", err)
	}
	temporary, err := os.CreateTemp(directory, ".interface-counters-*.tmp")
	if err != nil {
		return fmt.Errorf("create interface counter checkpoint: %w", err)
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
		return fmt.Errorf("secure interface counter checkpoint: %w", err)
	}
	if err := json.NewEncoder(temporary).Encode(checkpoint); err != nil {
		_ = temporary.Close()
		return fmt.Errorf("encode interface counter checkpoint: %w", err)
	}
	if err := temporary.Sync(); err != nil {
		_ = temporary.Close()
		return fmt.Errorf("sync interface counter checkpoint: %w", err)
	}
	if err := temporary.Close(); err != nil {
		return fmt.Errorf("close interface counter checkpoint: %w", err)
	}
	if err := os.Rename(temporaryPath, store.path); err != nil {
		return fmt.Errorf("publish interface counter checkpoint: %w", err)
	}
	keepTemporary = false
	directoryHandle, err := os.Open(directory)
	if err != nil {
		return fmt.Errorf("open interface counter checkpoint directory: %w", err)
	}
	defer directoryHandle.Close()
	if err := directoryHandle.Sync(); err != nil {
		return fmt.Errorf("sync interface counter checkpoint directory: %w", err)
	}
	return nil
}

func validateCheckpoint(checkpoint Checkpoint) error {
	if checkpoint.SchemaVersion != checkpointVersion || checkpoint.NodeID == "" || checkpoint.Counters == nil {
		return errors.New("invalid interface counter checkpoint")
	}
	if checkpoint.PendingBatch != nil {
		if err := checkpoint.PendingBatch.Validate(); err != nil {
			return fmt.Errorf("validate pending interface batch: %w", err)
		}
		if checkpoint.PendingBatch.NodeID != checkpoint.NodeID {
			return errors.New("pending interface batch belongs to another node")
		}
	}
	return nil
}
