package v1

import metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

type PhysicalDatabaseSpec struct {
	// +kubebuilder:validation:MaxLength=63
	// +kubebuilder:validation:Pattern=`^[a-z0-9]([-a-z0-9]*[a-z0-9])?$`
	// +kubebuilder:validation:XValidation:rule="self == oldSelf",message="operatorNamespace is immutable"
	OperatorNamespace string `json:"operatorNamespace"`

	// +kubebuilder:validation:Pattern=`^[^\s:/?#]+://[^\s/?#]+`
	AdapterAddress string `json:"adapterAddress"`

	CredentialsSecretRef CredentialsSecretRef `json:"credentialsSecretRef"`
}

type CredentialsSecretRef struct {
	Name string `json:"name"`
}

type PhysicalDatabaseStatus struct {
	ObservedGeneration int64              `json:"observedGeneration,omitempty"`
	PhysicalDatabaseID string             `json:"physicalDatabaseId,omitempty"`
	Conditions         []metav1.Condition `json:"conditions,omitempty"`
}

// +kubebuilder:object:root=true
// +kubebuilder:subresource:status
type PhysicalDatabase struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata,omitempty"`

	Spec   PhysicalDatabaseSpec   `json:"spec,omitempty"`
	Status PhysicalDatabaseStatus `json:"status,omitempty"`
}

// +kubebuilder:object:root=true
type PhysicalDatabaseList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitempty"`

	Items []PhysicalDatabase `json:"items"`
}

func init() {
	SchemeBuilder.Register(&PhysicalDatabase{}, &PhysicalDatabaseList{})
}
