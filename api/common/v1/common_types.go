package v1

// +kubebuilder:object:generate=true
// +groupName=netcracker.com

// Storage Describes Storage that will be used by patroni
type Storage struct {
	// +kubebuilder:validation:Pattern=`^[0-9]+(m|Ki|Mi|Gi|Ti|Pi|Ei|k|M|G|T|P|E)$`
	Size         string   `json:"size,omitempty"`
	Type         string   `json:"type,omitempty"`
	StorageClass string   `json:"storageClass,omitempty"`
	Volumes      []string `json:"volumes,omitempty"`
	Nodes        []string `json:"nodes,omitempty"`
	Selectors    []string `json:"selectors,omitempty"`
	AccessModes  []string `json:"accessModes,omitempty"`
}
type CloudSql struct {
	Project        string `json:"project,omitempty"`
	Instance       string `json:"instance,omitempty"`
	AuthSecretName string `json:"authSecretName,omitempty"`
}
