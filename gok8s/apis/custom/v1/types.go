package v1

import (
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

const CVAPP = "cvapp"

// +genclient
// +genclient:noStatus
// +k8s:deepcopy-gen:interfaces=k8s.io/apimachinery/pkg/runtime.Object

// ContainerVersion is ContainerVersion resource
type ContainerVersion struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata,omitempty"`

	Spec   ContainerVersionSpec   `json:"spec"`
	Status ContainerVersionStatus `json:"status"`
}

// ContainerVersionSpec is ContainerVersionSpec
type ContainerVersionSpec struct {
	ImageRepo     string `json:"imageRepo"`
	Tag           string `json:"tag"`
	VersionSyntax string `json:"versionSyntax"`

	PollIntervalSeconds int `json:"pollIntervalSeconds"`
	LivenessSeconds     int `json:"livenessSeconds"`
	MaxAttempts         int `json:"maxAttempts"`

	Selector  map[string]string `json:"selector,omitempty" protobuf:"bytes,2,rep,name=selector"`
	Container ContainerSpec     `json:"container"`

	Strategy *StrategySpec `json:"strategy"`

	Config *ConfigSpec `json:"config"`
}

// ContainerSpec defines a name of container and option container level verification step
type ContainerSpec struct {
	Name   string        `json:"name"`
	Verify []*VerifySpec `json:"verify"`
}

// StrategySpec defines a rollout strategy and optional verification steps.
type StrategySpec struct {
	Kind          string         `json:"kind"`
	BlueGreen     *BlueGreenSpec `json:"blueGreen"`
	Verifications []VerifySpec   `json:"verifications"`
}

// BlueGreenSpec defines a strategy for rolling out a workload via a blue-green deployment.
type BlueGreenSpec struct {
	ServiceName             string   `json:"serviceName"`
	VerificationServiceName string   `json:"verificationServiceName"`
	LabelNames              []string `json:"labelNames"`
	ScaleDown               bool     `json:"scaleDown"`
	TimeoutSeconds          int      `json:"timeoutSeconds"`
}

// VerifySpec defines various verification types performed during a rollout.
type VerifySpec struct {
	Kind           string `json:"kind"`
	Image          string `json:"image"`
	TimeoutSeconds int    `json:"timeoutSeconds"`
}

// ConfigSpec is spec for Config resources
type ConfigSpec struct {
	Name string `json:"name"`
	Key  string `json:"key"`
}

// ContainerVersionStatus is status  for Deployment resources
type ContainerVersionStatus struct {
	Created bool `json:"deployed"`

	// FailedRollouts is map of failed versions and the number of failures.
	FailedRollouts map[string]int
}

// +k8s:deepcopy-gen:interfaces=k8s.io/apimachinery/pkg/runtime.Object

// ContainerVersionList is a list of ContainerVersion resources
type ContainerVersionList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata"`

	Items []ContainerVersion `json:"items"`
}
