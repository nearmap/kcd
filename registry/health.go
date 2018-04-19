package registry

import (
	"io/ioutil"
	"log"
	"os"
)

const syncInProgress = "/health/sync"

func CaptureSyncStart() {
	if err := ioutil.WriteFile(syncInProgress, []byte("Sync in progress"), 0755); err != nil {
		log.Printf("Failed to create state file %v", err)
	}
}

func CaptureSyncComplete() {
	if err := os.Remove(syncInProgress); err != nil {
		log.Printf("Failed to remove state file %v", err)
	}

}
