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
type RestoreTestSpec struct {
	// Provider is the backup tool that performs the restores.
	Provider Provider `json:"provider"`
	// BackupSelector chooses which backups the restores read from.
	BackupSelector BackupSelector `json:"backupSelector"`
	// RestoreCheckRef names the check to run against each restored volume.
	RestoreCheckRef RestoreCheckRef `json:"restoreCheckRef"`
	// Target is the scratch volume each run restores into, with the same meaning as Restore.spec.target.
	Target RestoreTarget `json:"target,omitempty"`
	// Triggers defines what starts a run.
	Triggers *Triggers `json:"triggers,omitempty"`
	// RestoreWindow is the deadline for each attempt.
	// +kubebuilder:default="45m"
	RestoreWindow metav1.Duration `json:"restoreWindow"`
	// Retries defines how failed attempts are retried.
	// +kubebuilder:default={}
	Retries Retries `json:"retries"`
	// KeepOnFailure keeps the check pod and restore objects when the final attempt fails.
	// +kubebuilder:default=false
	KeepOnFailure bool `json:"keepOnFailure,omitempty"`
	// OverlapPolicy decides what happens when a trigger fires during a run.
	// +kubebuilder:default=Skip
	OverlapPolicy OverlapPolicy `json:"overlapPolicy,omitempty"`
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

// HandledBackup identifies the backup an afterBackup trigger used last.
type HandledBackup struct {
	// Name is the backup name.
	Name string `json:"name"`
	// CreatedAt is the time the backup completed.
	CreatedAt metav1.Time `json:"createdAt"`
}

// RestoreTestStatus defines the observed state of RestoreTest.
type RestoreTestStatus struct {
	// LastRun is the outcome of the most recent run.
	LastRun *LastRun `json:"lastRun,omitempty"`
	// NextRun is when the next cron run is scheduled.
	NextRun *metav1.Time `json:"nextRun,omitempty"`
	// LastHandled is the newest backup an afterBackup trigger used. Only newer backups trigger a run.
	LastHandled *HandledBackup `json:"lastHandled,omitempty"`
	// RetainedResources lists objects kept after a failed run for debugging.
	RetainedResources []string `json:"retainedResources,omitempty"`
	// CurrentRestore is the Restore of the run in progress, empty when no run is in progress.
	CurrentRestore string `json:"currentRestore,omitempty"`
	// RetryAt is when the next attempt of the run in progress is created.
	RetryAt *metav1.Time `json:"retryAt,omitempty"`
	// PendingRun is the backup of the run queued behind the run in progress (overlap policy Queue).
	PendingRun string `json:"pendingRun,omitempty"`
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
