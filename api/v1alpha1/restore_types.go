package v1alpha1

import (
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

// Provider is the backup tool that performs a restore.
// +kubebuilder:validation:Enum=velero;volsync;kopiur
type Provider string

const (
	// ProviderVelero restores with Velero.
	ProviderVelero Provider = "velero"
	// ProviderVolSync restores with VolSync.
	ProviderVolSync Provider = "volsync"
	// ProviderKopiur restores with Kopiur.
	ProviderKopiur Provider = "kopiur"
)

// RestorePhase is the lifecycle phase of a Restore.
// +kubebuilder:validation:Enum=Pending;Running;Succeeded;Failed;Canceled
type RestorePhase string

const (
	// RestorePhasePending means the restore has not started yet.
	RestorePhasePending RestorePhase = "Pending"
	// RestorePhaseRunning means the restore is in progress.
	RestorePhaseRunning RestorePhase = "Running"
	// RestorePhaseSucceeded means the restore and check finished successfully.
	RestorePhaseSucceeded RestorePhase = "Succeeded"
	// RestorePhaseFailed means the restore failed after all attempts.
	RestorePhaseFailed RestorePhase = "Failed"
	// RestorePhaseCanceled means the restore was stopped before it finished.
	RestorePhaseCanceled RestorePhase = "Canceled"
)

// BackupRef points to a backup object in the provider's namespace.
type BackupRef struct {
	// Name is the name of the backup object.
	Name string `json:"name"`
	// Namespace is the namespace of the backup object.
	Namespace string `json:"namespace,omitempty"`
}

// RestoreSource identifies the backup a Restore reads from.
type RestoreSource struct {
	// BackupRef is the backup to restore.
	BackupRef BackupRef `json:"backupRef"`
}

// RestoreTarget describes the scratch volume a Restore writes into.
type RestoreTarget struct {
	// StorageClassName is the storage class of the scratch volume.
	StorageClassName string `json:"storageClassName,omitempty"`
	// Size is the capacity of the scratch volume.
	Size resource.Quantity `json:"size"`
}

// RestoreSpec defines the desired state of Restore.
type RestoreSpec struct {
	// Provider is the backup tool that performs the restore.
	Provider Provider `json:"provider"`
	// Source is the backup to restore from.
	Source RestoreSource `json:"source"`
	// Target is the scratch volume to restore into.
	Target RestoreTarget `json:"target"`
	// BlueprintRef is the RestoreBlueprint that checks the restored data.
	BlueprintRef BlueprintRef `json:"blueprintRef"`
	// RestoreWindow is the deadline for this attempt, covering the restore, image pulls and the check.
	// +kubebuilder:default="45m"
	RestoreWindow metav1.Duration `json:"restoreWindow,omitempty"`
	// KeepOnFailure keeps the check pod, the scratch volume and the native restore object when the attempt fails.
	// +kubebuilder:default=false
	KeepOnFailure bool `json:"keepOnFailure,omitempty"`
}

// ProviderRef identifies the native object created by the provider.
type ProviderRef struct {
	// Kind is the kind of the native object.
	Kind string `json:"kind,omitempty"`
	// Name is the name of the native object.
	Name string `json:"name,omitempty"`
	// Namespace is the namespace of the native object.
	Namespace string `json:"namespace,omitempty"`
}

// RestoreStatus defines the observed state of Restore.
type RestoreStatus struct {
	// Phase is the current lifecycle phase.
	Phase RestorePhase `json:"phase,omitempty"`
	// Attempt is the number of the current attempt, starting at 1.
	Attempt int32 `json:"attempt,omitempty"`
	// StartTime is when the first attempt started.
	StartTime *metav1.Time `json:"startTime,omitempty"`
	// Deadline is when the current attempt times out.
	Deadline *metav1.Time `json:"deadline,omitempty"`
	// ProviderRef is the native object created by the provider.
	ProviderRef *ProviderRef `json:"providerRef,omitempty"`
	// Message is a human-readable detail about the current phase.
	Message string `json:"message,omitempty"`
	// Conditions are the latest observations of the restore state.
	// +listType=map
	// +listMapKey=type
	Conditions []metav1.Condition `json:"conditions,omitempty"`
}

// +kubebuilder:object:root=true
// +kubebuilder:subresource:status
// +kubebuilder:resource:shortName=restore
// +kubebuilder:printcolumn:name="Provider",type=string,JSONPath=`.spec.provider`
// +kubebuilder:printcolumn:name="Phase",type=string,JSONPath=`.status.phase`
// +kubebuilder:printcolumn:name="Attempt",type=integer,JSONPath=`.status.attempt`
// +kubebuilder:printcolumn:name="Age",type=date,JSONPath=`.metadata.creationTimestamp`

// Restore is one restore run, manual or started by a RestoreTest.
type Restore struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata,omitempty"`

	// Spec is the desired state of the restore.
	Spec RestoreSpec `json:"spec"`
	// Status is the observed state of the restore.
	Status RestoreStatus `json:"status,omitempty"`
}

// +kubebuilder:object:root=true

// RestoreList contains a list of Restore.
type RestoreList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitempty"`
	Items           []Restore `json:"items"`
}

func init() {
	SchemeBuilder.Register(&Restore{}, &RestoreList{})
}
