# API Reference

## Packages
- [bret.linogics.io/v1alpha1](#bretlinogicsiov1alpha1)


## bret.linogics.io/v1alpha1

Package v1alpha1 contains API Schema definitions for the restore v1alpha1 API group.

### Resource Types
- [Restore](#restore)
- [RestoreCheck](#restorecheck)
- [RestoreTest](#restoretest)



#### AfterBackupTrigger



AfterBackupTrigger starts a run after a backup completes.



_Appears in:_
- [Triggers](#triggers)

| Field | Description | Default | Validation |
| --- | --- | --- | --- |
| `delay` _[Duration](https://kubernetes.io/docs/reference/generated/kubernetes-api/v1.33/#duration-v1-meta)_ | Delay is how long to wait after the backup completes before the run starts. |  |  |


#### AutoExpand



AutoExpand derives the scratch size from the source size: clamp(source * (100 + percentage) / 100, min, max).



_Appears in:_
- [RestoreTarget](#restoretarget)

| Field | Description | Default | Validation |
| --- | --- | --- | --- |
| `percentage` _integer_ | Percentage is the headroom added on top of the source size, in percent. |  |  |
| `min` _[Quantity](https://kubernetes.io/docs/reference/generated/kubernetes-api/v1.33/#quantity-resource-api)_ | Min is the lower bound of the derived size. |  |  |
| `max` _[Quantity](https://kubernetes.io/docs/reference/generated/kubernetes-api/v1.33/#quantity-resource-api)_ | Max is the upper bound of the derived size. |  |  |


#### BackupRef



BackupRef points to a backup object in the provider's namespace.



_Appears in:_
- [RestoreSource](#restoresource)

| Field | Description | Default | Validation |
| --- | --- | --- | --- |
| `name` _string_ | Name is the name of the backup object. |  |  |
| `namespace` _string_ | Namespace is the namespace of the backup object. |  |  |


#### BackupSelector



BackupSelector chooses backups by name or by labels.



_Appears in:_
- [RestoreTestSpec](#restoretestspec)

| Field | Description | Default | Validation |
| --- | --- | --- | --- |
| `name` _string_ | Name is the name of one backup object. |  |  |
| `labels` _object (keys:string, values:string)_ | Labels selects backup objects that carry all of these labels. |  |  |


#### HandledBackup



HandledBackup identifies the backup an afterBackup trigger used last.



_Appears in:_
- [RestoreTestStatus](#restoreteststatus)

| Field | Description | Default | Validation |
| --- | --- | --- | --- |
| `name` _string_ | Name is the backup name. |  |  |
| `createdAt` _[Time](https://kubernetes.io/docs/reference/generated/kubernetes-api/v1.33/#time-v1-meta)_ | CreatedAt is the time the backup completed. |  |  |


#### LastRun



LastRun records the outcome of the most recent run.



_Appears in:_
- [RestoreTestStatus](#restoreteststatus)

| Field | Description | Default | Validation |
| --- | --- | --- | --- |
| `time` _[Time](https://kubernetes.io/docs/reference/generated/kubernetes-api/v1.33/#time-v1-meta)_ | Time is when the run finished. |  |  |
| `result` _[RunResult](#runresult)_ | Result is the outcome of the run. |  | Enum: [Succeeded Failed] <br /> |
| `reason` _string_ | Reason explains the result. |  |  |


#### OverlapPolicy

_Underlying type:_ _string_

OverlapPolicy decides what happens when a trigger fires during a run.

_Validation:_
- Enum: [Skip Queue Replace]

_Appears in:_
- [RestoreTestSpec](#restoretestspec)

| Field | Description |
| --- | --- |
| `Skip` | OverlapPolicySkip drops the new trigger while a run is in progress.<br /> |
| `Queue` | OverlapPolicyQueue keeps one pending run and starts it after the current one.<br /> |
| `Replace` | OverlapPolicyReplace cancels the running attempt and starts a new one.<br /> |


#### Provider

_Underlying type:_ _string_

Provider is the backup tool that performs a restore.

_Validation:_
- Enum: [velero volsync kopiur]

_Appears in:_
- [RestoreSpec](#restorespec)
- [RestoreTestSpec](#restoretestspec)

| Field | Description |
| --- | --- |
| `velero` | ProviderVelero restores with Velero.<br /> |
| `volsync` | ProviderVolSync restores with VolSync.<br /> |
| `kopiur` | ProviderKopiur restores with Kopiur.<br /> |


#### ProviderRef



ProviderRef identifies the native object created by the provider.



_Appears in:_
- [RestoreStatus](#restorestatus)

| Field | Description | Default | Validation |
| --- | --- | --- | --- |
| `kind` _string_ | Kind is the kind of the native object. |  |  |
| `name` _string_ | Name is the name of the native object. |  |  |
| `namespace` _string_ | Namespace is the namespace of the native object. |  |  |


#### Restore



Restore is one restore run, manual or started by a RestoreTest.





| Field | Description | Default | Validation |
| --- | --- | --- | --- |
| `apiVersion` _string_ | `bret.linogics.io/v1alpha1` | | |
| `kind` _string_ | `Restore` | | |
| `kind` _string_ | Kind is a string value representing the REST resource this object represents.<br />Servers may infer this from the endpoint the client submits requests to.<br />Cannot be updated.<br />In CamelCase.<br />More info: https://git.k8s.io/community/contributors/devel/sig-architecture/api-conventions.md#types-kinds |  | Optional: \{\} <br /> |
| `apiVersion` _string_ | APIVersion defines the versioned schema of this representation of an object.<br />Servers should convert recognized schemas to the latest internal value, and<br />may reject unrecognized values.<br />More info: https://git.k8s.io/community/contributors/devel/sig-architecture/api-conventions.md#resources |  | Optional: \{\} <br /> |
| `metadata` _[ObjectMeta](https://kubernetes.io/docs/reference/generated/kubernetes-api/v1.33/#objectmeta-v1-meta)_ | Refer to Kubernetes API documentation for fields of `metadata`. |  |  |
| `spec` _[RestoreSpec](#restorespec)_ | Spec is the desired state of the restore. |  |  |
| `status` _[RestoreStatus](#restorestatus)_ | Status is the observed state of the restore. |  |  |


#### RestoreCheck



RestoreCheck is the check run against a restored volume.





| Field | Description | Default | Validation |
| --- | --- | --- | --- |
| `apiVersion` _string_ | `bret.linogics.io/v1alpha1` | | |
| `kind` _string_ | `RestoreCheck` | | |
| `kind` _string_ | Kind is a string value representing the REST resource this object represents.<br />Servers may infer this from the endpoint the client submits requests to.<br />Cannot be updated.<br />In CamelCase.<br />More info: https://git.k8s.io/community/contributors/devel/sig-architecture/api-conventions.md#types-kinds |  | Optional: \{\} <br /> |
| `apiVersion` _string_ | APIVersion defines the versioned schema of this representation of an object.<br />Servers should convert recognized schemas to the latest internal value, and<br />may reject unrecognized values.<br />More info: https://git.k8s.io/community/contributors/devel/sig-architecture/api-conventions.md#resources |  | Optional: \{\} <br /> |
| `metadata` _[ObjectMeta](https://kubernetes.io/docs/reference/generated/kubernetes-api/v1.33/#objectmeta-v1-meta)_ | Refer to Kubernetes API documentation for fields of `metadata`. |  |  |
| `spec` _[RestoreCheckSpec](#restorecheckspec)_ | Spec is the desired check pod template. |  |  |
| `status` _[RestoreCheckStatus](#restorecheckstatus)_ | Status is the observed state of the check. |  |  |


#### RestoreCheckRef



RestoreCheckRef names the RestoreCheck used for the check.



_Appears in:_
- [RestoreSpec](#restorespec)
- [RestoreTestSpec](#restoretestspec)

| Field | Description | Default | Validation |
| --- | --- | --- | --- |
| `name` _string_ | Name is the name of the RestoreCheck in the same namespace. |  |  |


#### RestoreCheckSpec



RestoreCheckSpec defines the desired check pod template.



_Appears in:_
- [RestoreCheck](#restorecheck)

| Field | Description | Default | Validation |
| --- | --- | --- | --- |
| `restoreMount` _string_ | RestoreMount is the mount path of the single restored volume in every container. |  |  |
| `restoredVolumes` _[RestoredVolume](#restoredvolume) array_ | RestoredVolumes lists several restored volumes, each with its own mount path. |  |  |
| `initContainers` _[Container](https://kubernetes.io/docs/reference/generated/kubernetes-api/v1.33/#container-v1-core) array_ | InitContainers run before the check containers. |  |  |
| `containers` _[Container](https://kubernetes.io/docs/reference/generated/kubernetes-api/v1.33/#container-v1-core) array_ | Containers run the check; exit code 0 means pass. |  | MinItems: 1 <br /> |
| `scratchVolumes` _[ScratchVolume](#scratchvolume) array_ | ScratchVolumes are PVCs created per attempt for staging space, deleted after the attempt. |  |  |
| `volumes` _[Volume](https://kubernetes.io/docs/reference/generated/kubernetes-api/v1.33/#volume-v1-core) array_ | Volumes are extra volumes for the check pod, such as ConfigMaps or Secrets. |  |  |
| `securityContext` _[PodSecurityContext](https://kubernetes.io/docs/reference/generated/kubernetes-api/v1.33/#podsecuritycontext-v1-core)_ | SecurityContext is the pod-level security context, for example fsGroup or runAsUser for all containers. |  |  |
| `serviceAccountName` _string_ | ServiceAccountName is the service account the check pod runs as. |  |  |
| `imagePullSecrets` _[LocalObjectReference](https://kubernetes.io/docs/reference/generated/kubernetes-api/v1.33/#localobjectreference-v1-core) array_ | ImagePullSecrets are secrets used to pull the check images from private registries. |  |  |
| `nodeSelector` _object (keys:string, values:string)_ | NodeSelector constrains the check pod to nodes with these labels. |  |  |
| `tolerations` _[Toleration](https://kubernetes.io/docs/reference/generated/kubernetes-api/v1.33/#toleration-v1-core) array_ | Tolerations allow the check pod to run on tainted nodes. |  |  |
| `affinity` _[Affinity](https://kubernetes.io/docs/reference/generated/kubernetes-api/v1.33/#affinity-v1-core)_ | Affinity is the pod affinity and anti-affinity for the check pod. |  |  |
| `priorityClassName` _string_ | PriorityClassName is the priority class of the check pod. |  |  |
| `runtimeClassName` _string_ | RuntimeClassName is the runtime class of the check pod. |  |  |


#### RestoreCheckStatus



RestoreCheckStatus defines the observed state of RestoreCheck.



_Appears in:_
- [RestoreCheck](#restorecheck)

| Field | Description | Default | Validation |
| --- | --- | --- | --- |
| `conditions` _[Condition](https://kubernetes.io/docs/reference/generated/kubernetes-api/v1.33/#condition-v1-meta) array_ | Conditions are the latest observations of the check state. |  |  |
| `observedGeneration` _integer_ | ObservedGeneration is the generation last processed by the controller. |  |  |


#### RestorePhase

_Underlying type:_ _string_

RestorePhase is the lifecycle phase of a Restore.

_Validation:_
- Enum: [Pending Running Succeeded Failed Canceled]

_Appears in:_
- [RestoreStatus](#restorestatus)

| Field | Description |
| --- | --- |
| `Pending` | RestorePhasePending means the restore has not started yet.<br /> |
| `Running` | RestorePhaseRunning means the restore is in progress.<br /> |
| `Succeeded` | RestorePhaseSucceeded means the restore and check finished successfully.<br /> |
| `Failed` | RestorePhaseFailed means the restore failed after all attempts.<br /> |
| `Canceled` | RestorePhaseCanceled means the restore was stopped before it finished.<br /> |


#### RestoreSource



RestoreSource identifies the backup a Restore reads from.



_Appears in:_
- [RestoreSpec](#restorespec)

| Field | Description | Default | Validation |
| --- | --- | --- | --- |
| `backupRef` _[BackupRef](#backupref)_ | BackupRef is the backup to restore. |  |  |


#### RestoreSpec



RestoreSpec defines the desired state of Restore.



_Appears in:_
- [Restore](#restore)

| Field | Description | Default | Validation |
| --- | --- | --- | --- |
| `provider` _[Provider](#provider)_ | Provider is the backup tool that performs the restore. |  | Enum: [velero volsync kopiur] <br /> |
| `source` _[RestoreSource](#restoresource)_ | Source is the backup to restore from. |  |  |
| `target` _[RestoreTarget](#restoretarget)_ | Target is the scratch volume to restore into. |  |  |
| `restoreCheckRef` _[RestoreCheckRef](#restorecheckref)_ | RestoreCheckRef is the RestoreCheck that checks the restored data. |  |  |
| `restoreWindow` _[Duration](https://kubernetes.io/docs/reference/generated/kubernetes-api/v1.33/#duration-v1-meta)_ | RestoreWindow is the deadline for this attempt, covering the restore, image pulls and the check. | 45m |  |
| `keepOnFailure` _boolean_ | KeepOnFailure keeps the check pod, the scratch volume and the native restore object when the attempt fails. | false |  |


#### RestoreStatus



RestoreStatus defines the observed state of Restore.



_Appears in:_
- [Restore](#restore)

| Field | Description | Default | Validation |
| --- | --- | --- | --- |
| `phase` _[RestorePhase](#restorephase)_ | Phase is the current lifecycle phase. |  | Enum: [Pending Running Succeeded Failed Canceled] <br /> |
| `attempt` _integer_ | Attempt is the number of the current attempt, starting at 1. |  |  |
| `startTime` _[Time](https://kubernetes.io/docs/reference/generated/kubernetes-api/v1.33/#time-v1-meta)_ | StartTime is when the first attempt started. |  |  |
| `deadline` _[Time](https://kubernetes.io/docs/reference/generated/kubernetes-api/v1.33/#time-v1-meta)_ | Deadline is when the current attempt times out. |  |  |
| `providerRef` _[ProviderRef](#providerref)_ | ProviderRef is the native object created by the provider. |  |  |
| `resolvedSize` _[Quantity](https://kubernetes.io/docs/reference/generated/kubernetes-api/v1.33/#quantity-resource-api)_ | ResolvedSize is the scratch size resolved on the first reconcile. Later reconciles reuse it. |  |  |
| `message` _string_ | Message is a human-readable detail about the current phase. |  |  |
| `conditions` _[Condition](https://kubernetes.io/docs/reference/generated/kubernetes-api/v1.33/#condition-v1-meta) array_ | Conditions are the latest observations of the restore state. |  |  |


#### RestoreTarget



RestoreTarget describes the scratch volume a Restore writes into.



_Appears in:_
- [RestoreSpec](#restorespec)
- [RestoreTestSpec](#restoretestspec)

| Field | Description | Default | Validation |
| --- | --- | --- | --- |
| `storageClassName` _string_ | StorageClassName is the storage class of the scratch volume. |  |  |
| `size` _[Quantity](https://kubernetes.io/docs/reference/generated/kubernetes-api/v1.33/#quantity-resource-api)_ | Size is the capacity of the scratch volume. When unset, it is derived from the source size. |  |  |
| `autoExpand` _[AutoExpand](#autoexpand)_ | AutoExpand derives the size from the source size when Size is unset. |  |  |


#### RestoreTest



RestoreTest schedules restore runs and sets their policy.





| Field | Description | Default | Validation |
| --- | --- | --- | --- |
| `apiVersion` _string_ | `bret.linogics.io/v1alpha1` | | |
| `kind` _string_ | `RestoreTest` | | |
| `kind` _string_ | Kind is a string value representing the REST resource this object represents.<br />Servers may infer this from the endpoint the client submits requests to.<br />Cannot be updated.<br />In CamelCase.<br />More info: https://git.k8s.io/community/contributors/devel/sig-architecture/api-conventions.md#types-kinds |  | Optional: \{\} <br /> |
| `apiVersion` _string_ | APIVersion defines the versioned schema of this representation of an object.<br />Servers should convert recognized schemas to the latest internal value, and<br />may reject unrecognized values.<br />More info: https://git.k8s.io/community/contributors/devel/sig-architecture/api-conventions.md#resources |  | Optional: \{\} <br /> |
| `metadata` _[ObjectMeta](https://kubernetes.io/docs/reference/generated/kubernetes-api/v1.33/#objectmeta-v1-meta)_ | Refer to Kubernetes API documentation for fields of `metadata`. |  |  |
| `spec` _[RestoreTestSpec](#restoretestspec)_ | Spec is the desired state of the test. |  |  |
| `status` _[RestoreTestStatus](#restoreteststatus)_ | Status is the observed state of the test. |  |  |


#### RestoreTestSpec



RestoreTestSpec defines the desired state of RestoreTest.



_Appears in:_
- [RestoreTest](#restoretest)

| Field | Description | Default | Validation |
| --- | --- | --- | --- |
| `provider` _[Provider](#provider)_ | Provider is the backup tool that performs the restores. |  | Enum: [velero volsync kopiur] <br /> |
| `backupSelector` _[BackupSelector](#backupselector)_ | BackupSelector chooses which backups the restores read from. |  |  |
| `restoreCheckRef` _[RestoreCheckRef](#restorecheckref)_ | RestoreCheckRef names the check to run against each restored volume. |  |  |
| `target` _[RestoreTarget](#restoretarget)_ | Target is the scratch volume each run restores into, with the same meaning as Restore.spec.target. |  |  |
| `triggers` _[Triggers](#triggers)_ | Triggers defines what starts a run. |  |  |
| `restoreWindow` _[Duration](https://kubernetes.io/docs/reference/generated/kubernetes-api/v1.33/#duration-v1-meta)_ | RestoreWindow is the deadline for each attempt. | 45m |  |
| `retries` _[Retries](#retries)_ | Retries defines how failed attempts are retried. | \{  \} |  |
| `keepOnFailure` _boolean_ | KeepOnFailure keeps the check pod and restore objects when the final attempt fails. | false |  |
| `overlapPolicy` _[OverlapPolicy](#overlappolicy)_ | OverlapPolicy decides what happens when a trigger fires during a run. | Skip | Enum: [Skip Queue Replace] <br /> |


#### RestoreTestStatus



RestoreTestStatus defines the observed state of RestoreTest.



_Appears in:_
- [RestoreTest](#restoretest)

| Field | Description | Default | Validation |
| --- | --- | --- | --- |
| `lastRun` _[LastRun](#lastrun)_ | LastRun is the outcome of the most recent run. |  |  |
| `nextRun` _[Time](https://kubernetes.io/docs/reference/generated/kubernetes-api/v1.33/#time-v1-meta)_ | NextRun is when the next cron run is scheduled. |  |  |
| `lastHandled` _[HandledBackup](#handledbackup)_ | LastHandled is the newest backup an afterBackup trigger used. Only newer backups trigger a run. |  |  |
| `retainedResources` _string array_ | RetainedResources lists objects kept after a failed run for debugging. |  |  |
| `currentRestore` _string_ | CurrentRestore is the Restore of the run in progress, empty when no run is in progress. |  |  |
| `retryAt` _[Time](https://kubernetes.io/docs/reference/generated/kubernetes-api/v1.33/#time-v1-meta)_ | RetryAt is when the next attempt of the run in progress is created. |  |  |
| `pendingRun` _string_ | PendingRun is the backup of the run queued behind the run in progress (overlap policy Queue). |  |  |
| `lastRunNowValue` _string_ | LastRunNowValue is the last value of the run-now annotation that started a run. |  |  |
| `conditions` _[Condition](https://kubernetes.io/docs/reference/generated/kubernetes-api/v1.33/#condition-v1-meta) array_ | Conditions are the latest observations of the test state. |  |  |


#### RestoredVolume



RestoredVolume maps one restored PVC to a mount path in the check pod.



_Appears in:_
- [RestoreCheckSpec](#restorecheckspec)

| Field | Description | Default | Validation |
| --- | --- | --- | --- |
| `name` _string_ | Name is the name of the restored volume entry. |  |  |
| `mountPath` _string_ | MountPath is where the restored volume is mounted in every container. |  |  |
| `sourcePVC` _string_ | SourcePVC is the name of the PVC in the backed-up namespace that this volume restores. |  | MinLength: 1 <br /> |


#### Retries



Retries defines how often a failed attempt is retried.



_Appears in:_
- [RestoreTestSpec](#restoretestspec)

| Field | Description | Default | Validation |
| --- | --- | --- | --- |
| `count` _integer_ | Count is the number of retries after the first attempt. | 0 | Maximum: 10 <br />Minimum: 0 <br /> |
| `backoff` _[Duration](https://kubernetes.io/docs/reference/generated/kubernetes-api/v1.33/#duration-v1-meta)_ | Backoff is the wait between attempts. | 5m |  |


#### RunResult

_Underlying type:_ _string_

RunResult is the outcome of the last run of a RestoreTest.

_Validation:_
- Enum: [Succeeded Failed]

_Appears in:_
- [LastRun](#lastrun)

| Field | Description |
| --- | --- |
| `Succeeded` | RunResultSucceeded means the last run passed.<br /> |
| `Failed` | RunResultFailed means the last run failed.<br /> |


#### ScratchVolume



ScratchVolume is a PVC the operator creates for each attempt, for staging space the check needs.



_Appears in:_
- [RestoreCheckSpec](#restorecheckspec)

| Field | Description | Default | Validation |
| --- | --- | --- | --- |
| `name` _string_ | Name is the name of the scratch volume. |  | MinLength: 1 <br /> |
| `mountPath` _string_ | MountPath is where the scratch volume is mounted in every container. |  |  |
| `storageClassName` _string_ | StorageClassName is the storage class for the scratch PVC. Empty means the cluster default. |  |  |
| `size` _[Quantity](https://kubernetes.io/docs/reference/generated/kubernetes-api/v1.33/#quantity-resource-api)_ | Size is the requested capacity of the scratch PVC, for example 20Gi. |  |  |


#### Triggers



Triggers defines what starts a run.



_Appears in:_
- [RestoreTestSpec](#restoretestspec)

| Field | Description | Default | Validation |
| --- | --- | --- | --- |
| `cron` _string_ | Cron is a 5-field cron schedule evaluated in UTC. |  | Pattern: `^(\S+\s+)\{4\}\S+$` <br /> |
| `afterBackup` _[AfterBackupTrigger](#afterbackuptrigger)_ | AfterBackup starts a run after a matching backup completes. |  |  |
| `manual` _boolean_ | Manual enables starting a run by changing the run-now annotation. | false |  |


