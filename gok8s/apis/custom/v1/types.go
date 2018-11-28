package v1

import (
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

const KCDAPP = "kcdapp"

// +genclient
// +genclient:noStatus
// +k8s:deepcopy-gen:interfaces=k8s.io/apimachinery/pkg/runtime.Object

// KCD is KCD resource
type KCD struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata,omitempty"`

	Spec   KCDSpec   `json:"spec"`
	Status KCDStatus `json:"status"`
}

// KCDSpec is KCDSpec
type KCDSpec struct {
	ImageRepo     string `json:"imageRepo"`
	Tag           string `json:"tag"`
	VersionSyntax string `json:"versionSyntax"`

	PollIntervalSeconds int `json:"pollIntervalSeconds"`
	LivenessSeconds     int `json:"livenessSeconds"`
	TimeoutSeconds      int `json:"timeoutSeconds"`

	Selector  map[string]string `json:"selector,omitempty" protobuf:"bytes,2,rep,name=selector"`
	Container ContainerSpec     `json:"container"`

	Strategy StrategySpec `json:"strategy"`

	History  HistorySpec  `json:"history"`
	Rollback RollbackSpec `json:"rollback"`

	Config *ConfigSpec `json:"config"`
}

// ContainerSpec defines a name of container and option container level verification step
type ContainerSpec struct {
	Name   string       `json:"name"`
	Verify []VerifySpec `json:"verify"`
}

// StrategySpec defines a rollout strategy and optional verification steps.
type StrategySpec struct {
	Kind      string         `json:"kind"`
	BlueGreen *BlueGreenSpec `json:"blueGreen"`
	Verify    []VerifySpec   `json:"verify"`
}

// BlueGreenSpec defines a strategy for rolling out a workload via a blue-green deployment.
type BlueGreenSpec struct {
	ServiceName             string   `json:"serviceName"`
	VerificationServiceName string   `json:"verificationServiceName"`
	LabelNames              []string `json:"labelNames"`
	ScaleDown               bool     `json:"scaleDown"`
}

// VerifySpec defines various verification types performed during a rollout.
type VerifySpec struct {
	Kind  string `json:"kind"`
	Image string `json:"image"`
	Tag   string `json:"tag"`
}

// HistorySpec contains configuration for saving rollout history.
type HistorySpec struct {
	Enabled bool   `json:"enabled"`
	Name    string `json:"name"`
}

// RollbackSpec contains configuration for checking and rolling back failed deployments.
type RollbackSpec struct {
	Enabled bool `json:"enabled"`
}

// ConfigSpec is spec for Config resources
type ConfigSpec struct {
	Name string `json:"name"`
	Key  string `json:"key"`
}

// KCDStatus is status  for Deployment resources
type KCDStatus struct {
	Created bool `json:"deployed"`

	// CurrVersion is the most recent version of a rollout, which has a status.
	// CurrStatusTime is the time of the last status change.
	CurrVersion    string      `json:"currVersion"`
	CurrStatus     string      `json:"currStatus"`
	CurrStatusTime metav1.Time `json:"currStatusTime"`

	// SuccessVersion is the last version that was successfully deployed.
	SuccessVersion string `json:"successVersion"`
}

// +k8s:deepcopy-gen:interfaces=k8s.io/apimachinery/pkg/runtime.Object

// KCDList is a list of KCD resources
type KCDList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata"`

	Items []KCD `json:"items"`
}
