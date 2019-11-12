package v1alpha1

import (
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

// EDIT THIS FILE!  THIS IS SCAFFOLDING FOR YOU TO OWN!
// NOTE: json tags are required.  Any new fields you add must have json tags for the fields to be serialized.

// RdbcSpec defines the desired state of Rdbc
// +k8s:openapi-gen=true
type RdbcSpec struct {
	DbId     int32  `json:"dbId"`
	Name     string `json:"name"`
	Size     int    `json:"size"`
	Password string `json:"password"`
}

// RdbcStatus defines the observed state of Rdbc
// +k8s:openapi-gen=true
type RdbcStatus struct {
	DbEndpointUrl string  `json:"dbEndpointUrl"`
	DbUid         float64 `json:"dbUid"`
	Message       string  `json:"message"`
}

// +k8s:deepcopy-gen:interfaces=k8s.io/apimachinery/pkg/runtime.Object

// Rdbc is the Schema for the rdbcs API
// +k8s:openapi-gen=true
// +kubebuilder:subresource:status
type Rdbc struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata,omitempty"`

	Spec   RdbcSpec   `json:"spec,omitempty"`
	Status RdbcStatus `json:"status,omitempty"`
}

// +k8s:deepcopy-gen:interfaces=k8s.io/apimachinery/pkg/runtime.Object

// RdbcList contains a list of Rdbc
type RdbcList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitempty"`
	Items           []Rdbc `json:"items"`
}

func init() {
	SchemeBuilder.Register(&Rdbc{}, &RdbcList{})
}
