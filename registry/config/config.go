package config

import (
	"errors"
	"regexp"
)

var configRule, _ = regexp.Compile("([0-9A-Za-z_]*)/([0-9A-Za-z_]*)")

// SyncConfig describes the arguments required by Docker Registry (DR) Syncer
type SyncConfig struct {
	Freq    int
	Tag     string
	RepoARN string

	Deployment string
	Container  string

	ConfigKey string

	AccountID string
	RepoName  string

	ConfigMap *ConfigKey
}

// ConfigKey reflects key value pair of a config that maps
// directly with ConfigMap entry
type ConfigKey struct {
	Name string
	Key  string
}

func (sc *SyncConfig) Validate() bool {
	return sc.RepoARN != "" && sc.Tag != "" && sc.Deployment != "" && sc.Container != ""
}

func ConfigMap(namespace, key string) (*ConfigKey, error) {
	if key == "" {
		return nil, nil
	}
	cms := configRule.FindStringSubmatch(key)
	if len(cms) != 3 {
		return nil, errors.New("Invalid config map key for version passed")
	}
	return &ConfigKey{
		Name: cms[1],
		Key:  cms[2],
	}, nil
}
