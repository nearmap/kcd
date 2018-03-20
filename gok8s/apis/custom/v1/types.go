package v1

import (
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

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

	Deployment DeploymentSpec `json:"deployment"`

	Config *ConfigSpec `json:"config"`
}

// DeploymentSpec is spec for Deployment resources
type DeploymentSpec struct {
	Name      string `json:"name"`
	Container string `json:"container"`
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
