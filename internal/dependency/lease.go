package dependency

import (
	"os"
	"path/filepath"
	"sync"
	"sync/atomic"

	"noraegaori/internal/logger"
)

type Binary struct {
	Tool     string
	Version  string
	Path     string
	isSystem bool
	holders  atomic.Int32
	retired  atomic.Bool
}

func (binary *Binary) acquire() *Binary {
	binary.holders.Add(1)
	return binary
}

func (binary *Binary) Release() {
	if binary.holders.Add(-1) == 0 && binary.retired.Load() {
		removeRetired()
	}
}

var (
	retiredMu       sync.Mutex
	retiredBinaries []*Binary
	removeDirectory = os.RemoveAll
)

func retire(binary *Binary) {
	if binary == nil || binary.isSystem {
		return
	}

	binary.retired.Store(true)
	retiredMu.Lock()
	retiredBinaries = append(retiredBinaries, binary)
	retiredMu.Unlock()
}

func removeRetired() {
	retiredMu.Lock()
	defer retiredMu.Unlock()

	kept := retiredBinaries[:0]
	for _, binary := range retiredBinaries {
		if isActivePath(binary.Path) {
			continue
		}
		if binary.holders.Load() > 0 {
			kept = append(kept, binary)
			continue
		}

		directory := filepath.Dir(binary.Path)
		if err := removeDirectory(directory); err != nil {
			logger.Debugf("Failed to remove the retired %s %s, retrying later: %v", binary.Tool, binary.Version, err)
			kept = append(kept, binary)
			continue
		}
		logger.Debugf("Removed the retired %s %s", binary.Tool, binary.Version)
	}
	retiredBinaries = kept
}

type slot struct {
	active atomic.Pointer[Binary]
}

func (s *slot) current() *Binary {
	return s.active.Load()
}

func (s *slot) replace(next *Binary) {
	previous := s.active.Swap(next)
	if previous != nil && previous != next && (next == nil || previous.Path != next.Path) {
		retire(previous)
	}
}

var (
	ffmpegSlot    slot
	jsRuntimeSlot slot
)

func isActivePath(path string) bool {
	for _, s := range []*slot{&ffmpegSlot, &jsRuntimeSlot} {
		if current := s.current(); current != nil && current.Path == path {
			return true
		}
	}
	return false
}
