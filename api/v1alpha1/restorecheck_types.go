package v1alpha1

import (
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

// RestoredVolume maps one restored PVC to a mount path in the check pod.
type RestoredVolume struct {
	// SourcePVC is the name of the PVC in the backed-up namespace that this volume restores.
	// +kubebuilder:validation:Pattern=`^[a-z0-9]([-a-z0-9]*[a-z0-9])?$`
	// +kubebuilder:validation:MaxLength=63
	SourcePVC string `json:"sourcePVC"`
	// MountPath is where the restored volume is mounted in every container.
	// +kubebuilder:validation:MinLength=1
	MountPath string `json:"mountPath"`
}

// ScratchVolume is a PVC that the operator creates for each attempt. It provides staging space for the check.
type ScratchVolume struct {
	// Name is the name of the scratch volume.
	// +kubebuilder:validation:MinLength=1
	// +kubebuilder:validation:Pattern=`^[a-z0-9]([-a-z0-9]*[a-z0-9])?$`
	Name string `json:"name"`
	// MountPath is where the scratch volume is mounted in every container.
	MountPath string `json:"mountPath"`
	// StorageClassName is the storage class for the scratch PVC. Empty means the cluster default.
	StorageClassName string `json:"storageClassName,omitempty"`
	// Size is the requested capacity of the scratch PVC, for example 20Gi.
	Size resource.Quantity `json:"size"`
}

// RestoreCheckSpec defines the desired check pod template.
type RestoreCheckSpec struct {
	// RestoredVolumes lists the restored volumes, each with its own mount path.
	// +kubebuilder:validation:MinItems=1
	// +listType=map
	// +listMapKey=sourcePVC
	RestoredVolumes []RestoredVolume `json:"restoredVolumes"`
	// InitContainers run before the check containers.
	InitContainers []corev1.Container `json:"initContainers,omitempty"`
	// Containers run the check. Exit code 0 means pass.
	// +kubebuilder:validation:MinItems=1
	Containers []corev1.Container `json:"containers"`
	// ScratchVolumes are PVCs created for each attempt. They provide staging space and are deleted after the attempt.
	ScratchVolumes []ScratchVolume `json:"scratchVolumes,omitempty"`
	// Volumes are extra volumes for the check pod. For example, ConfigMaps or Secrets.
	Volumes []corev1.Volume `json:"volumes,omitempty"`

	// SecurityContext is the pod-level security context. For example, fsGroup or runAsUser applies to all containers.
	SecurityContext *corev1.PodSecurityContext `json:"securityContext,omitempty"`
	// ServiceAccountName is the service account the check pod runs as.
	ServiceAccountName string `json:"serviceAccountName,omitempty"`
	// AutomountServiceAccountToken mounts the service account token in the check pod. Unset means false.
	AutomountServiceAccountToken *bool `json:"automountServiceAccountToken,omitempty"`
	// ImagePullSecrets are secrets used to pull the check images from private registries.
	ImagePullSecrets []corev1.LocalObjectReference `json:"imagePullSecrets,omitempty"`
	// NodeSelector constrains the check pod to nodes with these labels.
	NodeSelector map[string]string `json:"nodeSelector,omitempty"`
	// Tolerations allow the check pod to run on tainted nodes.
	Tolerations []corev1.Toleration `json:"tolerations,omitempty"`
	// Affinity is the pod affinity and anti-affinity for the check pod.
	Affinity *corev1.Affinity `json:"affinity,omitempty"`
	// PriorityClassName is the priority class of the check pod.
	PriorityClassName string `json:"priorityClassName,omitempty"`
	// RuntimeClassName is the runtime class of the check pod.
	RuntimeClassName *string `json:"runtimeClassName,omitempty"`
}

// RestoreCheckStatus defines the observed state of RestoreCheck.
type RestoreCheckStatus struct {
	// Conditions are the latest observations of the check state.
	// +listType=map
	// +listMapKey=type
	Conditions []metav1.Condition `json:"conditions,omitempty"`
	// ObservedGeneration is the generation last processed by the controller.
	ObservedGeneration int64 `json:"observedGeneration,omitempty"`
}

// +kubebuilder:object:root=true
// +kubebuilder:subresource:status
// +kubebuilder:resource:shortName=rcheck

// RestoreCheck is the check run against a restored volume.
type RestoreCheck struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata,omitempty"`

	// Spec is the desired check pod template.
	Spec RestoreCheckSpec `json:"spec"`
	// Status is the observed state of the check.
	Status RestoreCheckStatus `json:"status,omitempty"`
}

// +kubebuilder:object:root=true

// RestoreCheckList contains a list of RestoreCheck.
type RestoreCheckList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitempty"`
	Items           []RestoreCheck `json:"items"`
}

func init() {
	SchemeBuilder.Register(&RestoreCheck{}, &RestoreCheckList{})
}
