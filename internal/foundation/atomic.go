package foundation

import (
	"fmt"
	"io/fs"
	"log"
	"os"
	"path/filepath"
	"time"
)

// AtomicWrite persists data to path using write-tmp → fsync → rename, creating
// the parent directory when missing. A reader of path never observes a partial
// write, and no tmp file is left behind on failure. Shared by GlobalConfigStore
// and internal/provision (D01 §4).
func AtomicWrite(path string, data []byte, perm fs.FileMode) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o750); err != nil {
		return fmt.Errorf("failed to create dir for %s: %w", path, err)
	}

	tmpPath := filepath.Clean(fmt.Sprintf("%s.tmp.%d.%d", path, os.Getpid(), time.Now().UnixNano()))
	f, err := os.OpenFile(tmpPath, os.O_WRONLY|os.O_CREATE|os.O_TRUNC, perm) // #nosec G304 -- caller-controlled path
	if err != nil {
		return fmt.Errorf("failed to create tmp file for %s: %w", path, err)
	}

	writeOK := false
	defer func() {
		if writeOK {
			return
		}
		if cErr := f.Close(); cErr != nil {
			log.Printf("[atomic] failed to close tmp file on cleanup: %v", cErr)
		}
		if rErr := os.Remove(tmpPath); rErr != nil {
			log.Printf("[atomic] failed to remove tmp file on cleanup: %v", rErr)
		}
	}()

	if _, wErr := f.Write(data); wErr != nil {
		return fmt.Errorf("failed to write %s: %w", path, wErr)
	}
	if sErr := f.Sync(); sErr != nil {
		return fmt.Errorf("failed to sync %s: %w", path, sErr)
	}
	if cErr := f.Close(); cErr != nil {
		return fmt.Errorf("failed to close %s: %w", path, cErr)
	}
	writeOK = true

	if rErr := os.Rename(tmpPath, path); rErr != nil {
		return fmt.Errorf("failed to rename %s: %w", path, rErr)
	}
	return nil
}
