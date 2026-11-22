package v1alpha1

import (
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

// OverlapPolicy decides what happens when a trigger fires during a run.
// +kubebuilder:validation:Enum=Skip;Queue;Replace
type OverlapPolicy string

const (
	// OverlapPolicySkip drops the new trigger while a run is in progress.
	OverlapPolicySkip OverlapPolicy = "Skip"
	// OverlapPolicyQueue keeps one pending run and starts it after the current one.
	OverlapPolicyQueue OverlapPolicy = "Queue"
	// OverlapPolicyReplace cancels the running attempt and starts a new one.
	OverlapPolicyReplace OverlapPolicy = "Replace"
)

// RunResult is the outcome of the last run of a RestoreTest.
// +kubebuilder:validation:Enum=Succeeded;Failed
type RunResult string

const (
	// RunResultSucceeded means the last run passed.
	RunResultSucceeded RunResult = "Succeeded"
	// RunResultFailed means the last run failed.
	RunResultFailed RunResult = "Failed"
)

// BackupSelector chooses backups by name or by labels.
// +kubebuilder:validation:XValidation:rule="has(self.name) || has(self.labels)",message="name or labels must be set"
type BackupSelector struct {
	// Name is the name of one backup object.
	Name string `json:"name,omitempty"`
	// Labels selects backup objects that carry all of these labels.
	Labels map[string]string `json:"labels,omitempty"`
}

// RestoreTestSource is one volume a RestoreTest restores: the PVC, the backups to pick from, and the scratch target.
type RestoreTestSource struct {
	// SourcePVC is the name of the PVC in the backed-up namespace.
	// +kubebuilder:validation:Pattern=`^[a-z0-9]([-a-z0-9]*[a-z0-9])?$`
	// +kubebuilder:validation:MaxLength=63
	SourcePVC string `json:"sourcePVC"`
	// BackupSelector chooses which backups the restore reads from.
	BackupSelector BackupSelector `json:"backupSelector"`
	// RestoreTarget is the scratch volume that each run restores into.
	RestoreTarget `json:",inline"`
}

// RestoreCheckRef names the RestoreCheck used for the check.
type RestoreCheckRef struct {
	// Name is the name of the RestoreCheck in the same namespace.
	Name string `json:"name"`
}

// AfterBackupTrigger starts a run after a backup completes.
type AfterBackupTrigger struct {
	// Delay is how long to wait after the backup completes before the run starts.
	Delay metav1.Duration `json:"delay,omitempty"`
}

// Triggers defines what starts a run.
type Triggers struct {
	// Cron is a 5-field cron schedule evaluated in UTC.
	// +kubebuilder:validation:Pattern=`^(\S+\s+){4}\S+$`
	Cron string `json:"cron,omitempty"`
	// AfterBackup starts a run after a matching backup completes.
	AfterBackup *AfterBackupTrigger `json:"afterBackup,omitempty"`
	// Manual enables starting a run by changing the run-now annotation.
	// +kubebuilder:default=false
	Manual bool `json:"manual,omitempty"`
}

// Retries defines how often a failed attempt is retried.
type Retries struct {
	// Count is the number of retries after the first attempt.
	// +kubebuilder:validation:Minimum=0
	// +kubebuilder:validation:Maximum=10
	// +kubebuilder:default=0
	Count int32 `json:"count,omitempty"`
	// Backoff is the wait between attempts.
	// +kubebuilder:default="5m"
	Backoff metav1.Duration `json:"backoff,omitempty"`
}

// RestoreTestSpec defines the desired state of RestoreTest.
// +kubebuilder:validation:XValidation:rule="self.provider != 'velero' || self.sources.all(s, s.backupSelector == self.sources[0].backupSelector && !has(s.size) && !has(s.autoExpand) && !has(s.storageClassName))",message="provider velero needs the same backupSelector on every source and no size, autoExpand or storageClassName"
// +kubebuilder:validation:XValidation:rule="self.provider == 'velero' || !has(self.velero)",message="spec.velero is only allowed with provider velero"
type RestoreTestSpec struct {
	// Provider is the backup tool that performs the restores.
	Provider Provider `json:"provider"`
	// Sources are the volumes each run restores.
	// +kubebuilder:validation:MinItems=1
	// +kubebuilder:validation:MaxItems=10
	// +listType=map
	// +listMapKey=sourcePVC
	Sources []RestoreTestSource `json:"sources"`
	// RestoreCheckRef names the check to run against each restored volume.
	RestoreCheckRef RestoreCheckRef `json:"restoreCheckRef"`
	// Velero narrows what each Velero restore brings back. Velero only.
	Velero *VeleroOptions `json:"velero,omitempty"`
	// Triggers defines what starts a run.
	Triggers *Triggers `json:"triggers,omitempty"`
	// RestoreWindow is the deadline for each attempt.
	// +kubebuilder:default="45m"
	// +kubebuilder:validation:XValidation:rule="duration(self) == duration('0s') || duration(self) >= duration('1m')",message="restoreWindow must be at least 1m, 0s means the default"
	RestoreWindow metav1.Duration `json:"restoreWindow"`
	// Retries defines how failed attempts are retried.
	// +kubebuilder:default={}
	Retries Retries `json:"retries"`
	// KeepOnFailure keeps the check pod and restore objects after the final attempt fails.
	// +kubebuilder:default=false
	KeepOnFailure bool `json:"keepOnFailure,omitempty"`
	// OverlapPolicy decides what happens when a trigger fires during a run.
	// +kubebuilder:default=Skip
	OverlapPolicy OverlapPolicy `json:"overlapPolicy,omitempty"`
	// SuccessfulRunsHistoryLimit is how many Restores of passed runs are kept. Older ones are deleted.
	// +kubebuilder:validation:Minimum=0
	// +kubebuilder:default=3
	SuccessfulRunsHistoryLimit *int32 `json:"successfulRunsHistoryLimit,omitempty"`
	// FailedRunsHistoryLimit is how many Restores of failed or canceled runs are kept. Older ones are deleted,
	// together with the objects they retained.
	// +kubebuilder:validation:Minimum=0
	// +kubebuilder:default=3
	FailedRunsHistoryLimit *int32 `json:"failedRunsHistoryLimit,omitempty"`
}

// LastRun records the outcome of the most recent run.
type LastRun struct {
	// Time is when the run finished.
	Time metav1.Time `json:"time"`
	// Result is the outcome of the run.
	Result RunResult `json:"result"`
	// Reason explains the result.
	Reason string `json:"reason,omitempty"`
}

// HandledBackup identifies the backup picked for one source.
type HandledBackup struct {
	// SourcePVC is the source this entry belongs to.
	SourcePVC string `json:"sourcePVC"`
	// Name is the backup name.
	Name string `json:"name"`
	// CreatedAt is the time the backup completed.
	CreatedAt metav1.Time `json:"createdAt"`
}

// RestoreTestStatus defines the observed state of RestoreTest.
type RestoreTestStatus struct {
	// LastRun is the outcome of the most recent run.
	LastRun *LastRun `json:"lastRun,omitempty"`
	// LastSuccess is the time of the last run that passed. It drives the success metric.
	LastSuccess *metav1.Time `json:"lastSuccess,omitempty"`
	// NextRun is when the next cron run is scheduled.
	NextRun *metav1.Time `json:"nextRun,omitempty"`
	// LastHandledBackups is the newest backup that an afterBackup trigger used. Only newer backups start a run.
	// +listType=map
	// +listMapKey=sourcePVC
	LastHandledBackups []HandledBackup `json:"lastHandledBackups,omitempty"`
	// RetainedResources lists the Restores whose objects were kept after a failed run. They help debugging.
	// A Restore that no longer exists is dropped from the list.
	RetainedResources []string `json:"retainedResources,omitempty"`
	// CurrentRestore is the Restore of the run in progress. It is empty when no run is in progress.
	CurrentRestore string `json:"currentRestore,omitempty"`
	// RetryAt is when the next attempt of the run in progress is created.
	RetryAt *metav1.Time `json:"retryAt,omitempty"`
	// PendingBackups are the backups of the queued run, one per source. The run waits for the run in progress.
	// It applies to overlap policy Queue.
	// +listType=map
	// +listMapKey=sourcePVC
	PendingBackups []HandledBackup `json:"pendingBackups,omitempty"`
	// PendingRunSource is the trigger source of the queued run. It keeps the name of the queued Restore stable across reconciles.
	PendingRunSource string `json:"pendingRunSource,omitempty"`
	// LastRunNowValue is the last value of the run-now annotation that started a run.
	LastRunNowValue string `json:"lastRunNowValue,omitempty"`
	// Conditions are the latest observations of the test state.
	// +listType=map
	// +listMapKey=type
	Conditions []metav1.Condition `json:"conditions,omitempty"`
}

// +kubebuilder:object:root=true
// +kubebuilder:subresource:status
// +kubebuilder:resource:shortName=rtest
// +kubebuilder:validation:XValidation:rule="size(self.metadata.name) <= 50",message="name must be at most 50 characters"
// +kubebuilder:printcolumn:name="Provider",type=string,JSONPath=`.spec.provider`
// +kubebuilder:printcolumn:name="Last",type=string,JSONPath=`.status.lastRun.result`
// +kubebuilder:printcolumn:name="Next",type=date,JSONPath=`.status.nextRun`
// +kubebuilder:printcolumn:name="Age",type=date,JSONPath=`.metadata.creationTimestamp`

// RestoreTest schedules restore runs and sets their policy.
type RestoreTest struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata,omitempty"`

	// Spec is the desired state of the test.
	Spec RestoreTestSpec `json:"spec"`
	// Status is the observed state of the test.
	Status RestoreTestStatus `json:"status,omitempty"`
}

// +kubebuilder:object:root=true

// RestoreTestList contains a list of RestoreTest.
type RestoreTestList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitempty"`
	Items           []RestoreTest `json:"items"`
}

func init() {
	SchemeBuilder.Register(&RestoreTest{}, &RestoreTestList{})
}
