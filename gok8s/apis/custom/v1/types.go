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
	ImageRepo      string `json:"imageRepo"`
	Tag            string `json:"tag"`
	CheckFrequency int    `json:"checkFrequency"`

	Strategy *StrategySpec `json:"strategy"`

	Selector map[string]string `json:"selector,omitempty" protobuf:"bytes,2,rep,name=selector"`

	Container string `json:"container"`

	Config *ConfigSpec `json:"config"`
}

type StrategySpec struct {
	Type   string      `json:"type"`
	Verify *VerifySpec `json:"verify"`
}

type VerifySpec struct {
	Image string `json:"image"`
}

// ConfigSpec is spec for Config resources
type ConfigSpec struct {
	Name string `json:"name"`
	Key  string `json:"key"`
}

// ContainerVersionStatus is status  for Deployment resources
type ContainerVersionStatus struct {
	Created bool `json:"deployed"`
}

// +k8s:deepcopy-gen:interfaces=k8s.io/apimachinery/pkg/runtime.Object

// ContainerVersionList is a list of ContainerVersion resources
type ContainerVersionList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata"`

	Items []ContainerVersion `json:"items"`
}
