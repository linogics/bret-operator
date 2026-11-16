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



AutoExpand derives the scratch size from the source size.
The formula is: clamp(source * (100 + percentage) / 100, min, max).



_Appears in:_
- [RestoreSource](#restoresource)
- [RestoreTarget](#restoretarget)
- [RestoreTestSource](#restoretestsource)

| Field | Description | Default | Validation |
| --- | --- | --- | --- |
| `percentage` _integer_ | Percentage is the headroom added to the source size, in percent. |  |  |
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
- [RestoreTestSource](#restoretestsource)

| Field | Description | Default | Validation |
| --- | --- | --- | --- |
| `name` _string_ | Name is the name of one backup object. |  |  |
| `labels` _object (keys:string, values:string)_ | Labels selects backup objects that carry all of these labels. |  |  |


#### HandledBackup



HandledBackup identifies the backup picked for one source.



_Appears in:_
- [RestoreTestStatus](#restoreteststatus)

| Field | Description | Default | Validation |
| --- | --- | --- | --- |
| `sourcePVC` _string_ | SourcePVC is the source this entry belongs to. |  |  |
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



Restore is one restore run. It is manual or started by a RestoreTest.





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
| `restoredVolumes` _[RestoredVolume](#restoredvolume) array_ | RestoredVolumes lists the restored volumes, each with its own mount path. |  | MinItems: 1 <br /> |
| `initContainers` _[Container](https://kubernetes.io/docs/reference/generated/kubernetes-api/v1.33/#container-v1-core) array_ | InitContainers run before the check containers. |  |  |
| `containers` _[Container](https://kubernetes.io/docs/reference/generated/kubernetes-api/v1.33/#container-v1-core) array_ | Containers run the check. Exit code 0 means pass. |  | MinItems: 1 <br /> |
| `scratchVolumes` _[ScratchVolume](#scratchvolume) array_ | ScratchVolumes are PVCs created for each attempt. They provide staging space and are deleted after the attempt. |  |  |
| `volumes` _[Volume](https://kubernetes.io/docs/reference/generated/kubernetes-api/v1.33/#volume-v1-core) array_ | Volumes are extra volumes for the check pod. For example, ConfigMaps or Secrets. |  |  |
| `securityContext` _[PodSecurityContext](https://kubernetes.io/docs/reference/generated/kubernetes-api/v1.33/#podsecuritycontext-v1-core)_ | SecurityContext is the pod-level security context. For example, fsGroup or runAsUser applies to all containers. |  |  |
| `serviceAccountName` _string_ | ServiceAccountName is the service account the check pod runs as. |  |  |
| `automountServiceAccountToken` _boolean_ | AutomountServiceAccountToken mounts the service account token in the check pod. Unset means false. |  |  |
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



RestoreSource identifies one backup a Restore reads from and the scratch volume it writes into.



_Appears in:_
- [RestoreSpec](#restorespec)

| Field | Description | Default | Validation |
| --- | --- | --- | --- |
| `sourcePVC` _string_ | SourcePVC is the name of the PVC in the backed-up namespace that this source restores. |  | MaxLength: 63 <br />Pattern: `^[a-z0-9]([-a-z0-9]*[a-z0-9])?$` <br /> |
| `backupRef` _[BackupRef](#backupref)_ | BackupRef is the backup to restore. Velero sources all name the same Backup. |  |  |
| `restoreAsOf` _[Time](https://kubernetes.io/docs/reference/generated/kubernetes-api/v1.33/#time-v1-meta)_ | RestoreAsOf restores the newest snapshot taken at or before this time. VolSync only.<br />Unset restores the newest snapshot. |  |  |
| `storageClassName` _string_ | StorageClassName is the storage class of the scratch volume. |  |  |
| `size` _[Quantity](https://kubernetes.io/docs/reference/generated/kubernetes-api/v1.33/#quantity-resource-api)_ | Size is the capacity of the scratch volume. The source size sets it when unset. |  |  |
| `autoExpand` _[AutoExpand](#autoexpand)_ | AutoExpand derives the size from the source size when Size is unset. |  |  |


#### RestoreSpec



RestoreSpec defines the desired state of Restore.



_Appears in:_
- [Restore](#restore)

| Field | Description | Default | Validation |
| --- | --- | --- | --- |
| `provider` _[Provider](#provider)_ | Provider is the backup tool that performs the restore. |  | Enum: [velero volsync kopiur] <br /> |
| `sources` _[RestoreSource](#restoresource) array_ | Sources are the backups to restore from, each with its scratch volume. |  | MaxItems: 10 <br />MinItems: 1 <br /> |
| `restoreCheckRef` _[RestoreCheckRef](#restorecheckref)_ | RestoreCheckRef is the RestoreCheck that checks the restored data. |  |  |
| `restoreWindow` _[Duration](https://kubernetes.io/docs/reference/generated/kubernetes-api/v1.33/#duration-v1-meta)_ | RestoreWindow is the deadline for this attempt. It covers the restore, image pulls and the check. | 45m |  |
| `velero` _[VeleroOptions](#velerooptions)_ | Velero narrows what a Velero restore brings back. Velero only. |  |  |
| `keepOnFailure` _boolean_ | KeepOnFailure keeps the check pod, scratch volume and native restore object after a failed attempt. | false |  |


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
| `providerRefs` _[ProviderRef](#providerref) array_ | ProviderRefs are the native objects created by the provider. |  |  |
| `resolvedSizes` _object (keys:string, values:[Quantity](https://kubernetes.io/docs/reference/generated/kubernetes-api/v1.33/#quantity-resource-api))_ | ResolvedSizes maps each sourcePVC to the scratch size resolved on the first reconcile. Later reconciles reuse it.<br />Velero uses the empty key. |  |  |
| `message` _string_ | Message is a human-readable detail about the current phase. |  |  |
| `conditions` _[Condition](https://kubernetes.io/docs/reference/generated/kubernetes-api/v1.33/#condition-v1-meta) array_ | Conditions are the latest observations of the restore state. |  |  |


#### RestoreTarget



RestoreTarget describes the scratch volume a Restore writes into.



_Appears in:_
- [RestoreSource](#restoresource)
- [RestoreTestSource](#restoretestsource)

| Field | Description | Default | Validation |
| --- | --- | --- | --- |
| `storageClassName` _string_ | StorageClassName is the storage class of the scratch volume. |  |  |
| `size` _[Quantity](https://kubernetes.io/docs/reference/generated/kubernetes-api/v1.33/#quantity-resource-api)_ | Size is the capacity of the scratch volume. The source size sets it when unset. |  |  |
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


#### RestoreTestSource



RestoreTestSource is one volume a RestoreTest restores: the PVC, the backups to pick from, and the scratch target.



_Appears in:_
- [RestoreTestSpec](#restoretestspec)

| Field | Description | Default | Validation |
| --- | --- | --- | --- |
| `sourcePVC` _string_ | SourcePVC is the name of the PVC in the backed-up namespace. |  | MaxLength: 63 <br />Pattern: `^[a-z0-9]([-a-z0-9]*[a-z0-9])?$` <br /> |
| `backupSelector` _[BackupSelector](#backupselector)_ | BackupSelector chooses which backups the restore reads from. |  |  |
| `storageClassName` _string_ | StorageClassName is the storage class of the scratch volume. |  |  |
| `size` _[Quantity](https://kubernetes.io/docs/reference/generated/kubernetes-api/v1.33/#quantity-resource-api)_ | Size is the capacity of the scratch volume. The source size sets it when unset. |  |  |
| `autoExpand` _[AutoExpand](#autoexpand)_ | AutoExpand derives the size from the source size when Size is unset. |  |  |


#### RestoreTestSpec



RestoreTestSpec defines the desired state of RestoreTest.



_Appears in:_
- [RestoreTest](#restoretest)

| Field | Description | Default | Validation |
| --- | --- | --- | --- |
| `provider` _[Provider](#provider)_ | Provider is the backup tool that performs the restores. |  | Enum: [velero volsync kopiur] <br /> |
| `sources` _[RestoreTestSource](#restoretestsource) array_ | Sources are the volumes each run restores. |  | MaxItems: 10 <br />MinItems: 1 <br /> |
| `restoreCheckRef` _[RestoreCheckRef](#restorecheckref)_ | RestoreCheckRef names the check to run against each restored volume. |  |  |
| `velero` _[VeleroOptions](#velerooptions)_ | Velero narrows what each Velero restore brings back. Velero only. |  |  |
| `triggers` _[Triggers](#triggers)_ | Triggers defines what starts a run. |  |  |
| `restoreWindow` _[Duration](https://kubernetes.io/docs/reference/generated/kubernetes-api/v1.33/#duration-v1-meta)_ | RestoreWindow is the deadline for each attempt. | 45m |  |
| `retries` _[Retries](#retries)_ | Retries defines how failed attempts are retried. | \{  \} |  |
| `keepOnFailure` _boolean_ | KeepOnFailure keeps the check pod and restore objects after the final attempt fails. | false |  |
| `overlapPolicy` _[OverlapPolicy](#overlappolicy)_ | OverlapPolicy decides what happens when a trigger fires during a run. | Skip | Enum: [Skip Queue Replace] <br /> |


#### RestoreTestStatus



RestoreTestStatus defines the observed state of RestoreTest.



_Appears in:_
- [RestoreTest](#restoretest)

| Field | Description | Default | Validation |
| --- | --- | --- | --- |
| `lastRun` _[LastRun](#lastrun)_ | LastRun is the outcome of the most recent run. |  |  |
| `lastSuccess` _[Time](https://kubernetes.io/docs/reference/generated/kubernetes-api/v1.33/#time-v1-meta)_ | LastSuccess is the time of the last run that passed. It drives the success metric. |  |  |
| `nextRun` _[Time](https://kubernetes.io/docs/reference/generated/kubernetes-api/v1.33/#time-v1-meta)_ | NextRun is when the next cron run is scheduled. |  |  |
| `lastHandledBackups` _[HandledBackup](#handledbackup) array_ | LastHandledBackups is the newest backup that an afterBackup trigger used. Only newer backups start a run. |  |  |
| `retainedResources` _string array_ | RetainedResources lists objects kept after a failed run. They help debugging. |  |  |
| `currentRestore` _string_ | CurrentRestore is the Restore of the run in progress. It is empty when no run is in progress. |  |  |
| `retryAt` _[Time](https://kubernetes.io/docs/reference/generated/kubernetes-api/v1.33/#time-v1-meta)_ | RetryAt is when the next attempt of the run in progress is created. |  |  |
| `pendingBackups` _[HandledBackup](#handledbackup) array_ | PendingBackups are the backups of the queued run, one per source. The run waits for the run in progress.<br />It applies to overlap policy Queue. |  |  |
| `pendingRunSource` _string_ | PendingRunSource is the trigger source of the queued run. It keeps the name of the queued Restore stable across reconciles. |  |  |
| `lastRunNowValue` _string_ | LastRunNowValue is the last value of the run-now annotation that started a run. |  |  |
| `conditions` _[Condition](https://kubernetes.io/docs/reference/generated/kubernetes-api/v1.33/#condition-v1-meta) array_ | Conditions are the latest observations of the test state. |  |  |


#### RestoredVolume



RestoredVolume maps one restored PVC to a mount path in the check pod.



_Appears in:_
- [RestoreCheckSpec](#restorecheckspec)

| Field | Description | Default | Validation |
| --- | --- | --- | --- |
| `sourcePVC` _string_ | SourcePVC is the name of the PVC in the backed-up namespace that this volume restores. |  | MaxLength: 63 <br />Pattern: `^[a-z0-9]([-a-z0-9]*[a-z0-9])?$` <br /> |
| `mountPath` _string_ | MountPath is where the restored volume is mounted in every container. |  | MinLength: 1 <br /> |


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



ScratchVolume is a PVC that the operator creates for each attempt. It provides staging space for the check.



_Appears in:_
- [RestoreCheckSpec](#restorecheckspec)

| Field | Description | Default | Validation |
| --- | --- | --- | --- |
| `name` _string_ | Name is the name of the scratch volume. |  | MinLength: 1 <br />Pattern: `^[a-z0-9]([-a-z0-9]*[a-z0-9])?$` <br /> |
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


#### VeleroOptions



VeleroOptions narrows what a Velero restore brings back.



_Appears in:_
- [RestoreSpec](#restorespec)
- [RestoreTestSpec](#restoretestspec)

| Field | Description | Default | Validation |
| --- | --- | --- | --- |
| `labelSelector` _[LabelSelector](https://kubernetes.io/docs/reference/generated/kubernetes-api/v1.33/#labelselector-v1-meta)_ | LabelSelector selects the namespace objects to restore. |  |  |
| `orLabelSelectors` _[LabelSelector](https://kubernetes.io/docs/reference/generated/kubernetes-api/v1.33/#labelselector-v1-meta) array_ | OrLabelSelectors select the namespace objects to restore, any one may match. |  |  |


