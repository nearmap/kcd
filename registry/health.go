package registry

import (
	"io/ioutil"
	"os"
	"time"

	"github.com/pkg/errors"
)

const syncInProgress = "/health/sync"

func SetSyncStatus() error {
	if err := ioutil.WriteFile(syncInProgress, []byte(""), 0755); err != nil {
		return err
	}
	return nil
}

func CleanupSyncStart() error {
	if err := os.Remove(syncInProgress); err != nil {
		return err
	}
	return nil
}

func SyncCheck(d time.Duration) error {
	f, err := os.Stat(syncInProgress)
	if err != nil {
		return err
	}

	if !time.Now().Add(d * -1).Before(f.ModTime()) {
		return errors.Errorf("Last sync run was %s", f.ModTime().String())
	}
	return nil
}
