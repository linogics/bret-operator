package v1alpha1

import (
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

// RestoredVolume maps one restored PVC to a mount path in the check pod.
type RestoredVolume struct {
	// Name is the name of the restored volume entry.
	Name string `json:"name"`
	// MountPath is where the restored volume is mounted in every container.
	MountPath string `json:"mountPath"`
}

// ScratchVolume is a PVC the operator creates for each attempt, for staging space the check needs.
type ScratchVolume struct {
	// Name is the name of the scratch volume.
	// +kubebuilder:validation:MinLength=1
	Name string `json:"name"`
	// MountPath is where the scratch volume is mounted in every container.
	MountPath string `json:"mountPath"`
	// StorageClassName is the storage class for the scratch PVC. Empty means the cluster default.
	StorageClassName string `json:"storageClassName,omitempty"`
	// Size is the requested capacity of the scratch PVC, for example 20Gi.
	Size resource.Quantity `json:"size"`
}

// RestoreBlueprintSpec defines the desired check pod template.
// +kubebuilder:validation:XValidation:rule="has(self.restoreMount) != has(self.restoredVolumes)",message="exactly one of restoreMount or restoredVolumes must be set"
type RestoreBlueprintSpec struct {
	// RestoreMount is the mount path of the single restored volume in every container.
	RestoreMount string `json:"restoreMount,omitempty"`
	// RestoredVolumes lists several restored volumes, each with its own mount path.
	RestoredVolumes []RestoredVolume `json:"restoredVolumes,omitempty"`
	// InitContainers run before the check containers.
	InitContainers []corev1.Container `json:"initContainers,omitempty"`
	// Containers run the check; exit code 0 means pass.
	// +kubebuilder:validation:MinItems=1
	Containers []corev1.Container `json:"containers"`
	// ScratchVolumes are PVCs created per attempt for staging space, deleted after the attempt.
	ScratchVolumes []ScratchVolume `json:"scratchVolumes,omitempty"`
	// Volumes are extra volumes for the check pod, such as ConfigMaps or Secrets.
	Volumes []corev1.Volume `json:"volumes,omitempty"`
}

// RestoreBlueprintStatus defines the observed state of RestoreBlueprint.
type RestoreBlueprintStatus struct {
	// Conditions are the latest observations of the blueprint state.
	// +listType=map
	// +listMapKey=type
	Conditions []metav1.Condition `json:"conditions,omitempty"`
	// ObservedGeneration is the generation last processed by the controller.
	ObservedGeneration int64 `json:"observedGeneration,omitempty"`
}

// +kubebuilder:object:root=true
// +kubebuilder:subresource:status
// +kubebuilder:resource:shortName=rblueprint

// RestoreBlueprint is the check run against a restored volume.
type RestoreBlueprint struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata,omitempty"`

	// Spec is the desired check pod template.
	Spec RestoreBlueprintSpec `json:"spec"`
	// Status is the observed state of the blueprint.
	Status RestoreBlueprintStatus `json:"status,omitempty"`
}

// +kubebuilder:object:root=true

// RestoreBlueprintList contains a list of RestoreBlueprint.
type RestoreBlueprintList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitempty"`
	Items           []RestoreBlueprint `json:"items"`
}

func init() {
	SchemeBuilder.Register(&RestoreBlueprint{}, &RestoreBlueprintList{})
}
