package state

import (
	"io/ioutil"
	"os"
	"time"

	"github.com/pkg/errors"
)

// updateSyncStatus updates the health status of the syncer to indicate that it is
// in a healthy state.
func (m *Machine) updateSyncStatus() error {
	if err := ioutil.WriteFile(m.options.HealthFilename, []byte(""), 0755); err != nil {
		return errors.Wrapf(err, "failed to write sync health file at %s", m.options.HealthFilename)
	}
	return nil
}

// CleanupSyncStatus removes the existing sync health state.
func (m *Machine) CleanupSyncStatus() error {
	if err := os.Remove(m.options.HealthFilename); err != nil {
		return errors.Wrapf(err, "failed to clean up sync status at %s", m.options.HealthFilename)
	}
	return nil
}

// CheckHealth checks that the last sync was run within the given time duration.
// Returns nil if the last sync run was run within the duration, and an error otherwise.
func (m *Machine) CheckHealth(d time.Duration) error {
	f, err := os.Stat(m.options.HealthFilename)
	if err != nil {
		return errors.Wrapf(err, "failed to stat the health status at %s", m.options.HealthFilename)
	}

	if !time.Now().Add(d * -1).Before(f.ModTime()) {
		return errors.Errorf("Last sync run was %s", f.ModTime().String())
	}
	return nil
}
