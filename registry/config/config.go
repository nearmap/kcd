package config

// SyncConfig describes the arguments required by Syncer
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

type ConfigKey struct {
	Name string
	Key  string
}

func (sc *SyncConfig) Validate() bool {
	return sc.RepoARN != "" && sc.Tag != "" && sc.Deployment != "" && sc.Container != ""
}
