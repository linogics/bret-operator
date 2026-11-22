package controller

import (
	"context"
	"reflect"
	"strings"
	"testing"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"

	restorev1alpha1 "github.com/linogics/bret-operator/api/v1alpha1"
)

func TestVeleroRestoreSpec(t *testing.T) {
	restore := sizeTestRestore("velero")
	obj := (&RestoreReconciler{}).veleroRestoreObject(restore)

	if obj.GetNamespace() != "velero" || obj.GetAPIVersion() != "velero.io/v1" || obj.GetKind() != "Restore" {
		t.Fatalf("unexpected identity %s %s/%s", obj.GetAPIVersion(), obj.GetNamespace(), obj.GetKind())
	}

	want := map[string]any{
		"backupName":         testBackupName,
		"includedNamespaces": []any{"ns"},
		"includedResources":  []any{"persistentvolumeclaims", "persistentvolumes"},
		"namespaceMapping":   map[string]any{"ns": veleroScratchName(restore)},
		"restorePVs":         true,
	}

	if got, _, _ := unstructured.NestedMap(obj.Object, "spec"); !reflect.DeepEqual(got, want) {
		t.Fatalf("spec = %v, want %v", got, want)
	}
}

func TestVeleroCheckRunsInScratchNamespace(t *testing.T) {
	for _, tt := range []struct {
		name      string
		podPhase  corev1.PodPhase
		keep      bool
		wantPhase restorev1alpha1.RestorePhase
		wantNS    bool
	}{
		{"success deletes scratch namespace", corev1.PodSucceeded, false, restorev1alpha1.RestorePhaseSucceeded, false},
		{"failure with keepOnFailure retains it", corev1.PodFailed, true, restorev1alpha1.RestorePhaseFailed, true},
	} {
		t.Run(tt.name, func(t *testing.T) {
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()

			restore := newTestRestore()
			restore.Spec.Provider = restorev1alpha1.ProviderVelero
			restore.Spec.KeepOnFailure = tt.keep

			c, r := fakeRestoreClient(t, restore, newTestCheck(), &corev1.Namespace{ObjectMeta: metav1.ObjectMeta{Name: testNamespace}})
			key := client.ObjectKeyFromObject(restore)

			reconcileRestore(t, r, key) // Pending
			reconcileRestore(t, r, key) // Velero Restore created

			vr := r.veleroStub(restore)
			if err := c.Get(ctx, client.ObjectKeyFromObject(vr), vr); err != nil {
				t.Fatalf("Velero restore not created: %v", err)
			}

			if err := unstructured.SetNestedField(vr.Object, "Completed", "status", "phase"); err != nil {
				t.Fatal(err)
			}

			if err := unstructured.SetNestedField(vr.Object, int64(1), "status", "progress", "itemsRestored"); err != nil {
				t.Fatal(err)
			}

			if err := c.Update(ctx, vr); err != nil {
				t.Fatal(err)
			}

			scratch := veleroScratchNamespace(restore)
			podKey := types.NamespacedName{Name: checkPodName(restore), Namespace: scratch.Name}

			reconcileRestore(t, r, key) // namespace absent: requeue, no error

			if err := c.Get(ctx, podKey, &corev1.Pod{}); !errors.IsNotFound(err) {
				t.Fatalf("pod created before the scratch namespace exists: %v", err)
			}

			pvc := &corev1.PersistentVolumeClaim{ObjectMeta: metav1.ObjectMeta{Name: testDataName, Namespace: scratch.Name}}
			if err := c.Create(ctx, scratch); err != nil {
				t.Fatal(err)
			}

			if err := c.Create(ctx, pvc); err != nil {
				t.Fatal(err)
			}

			reconcileRestore(t, r, key)

			pod := &corev1.Pod{}
			if err := c.Get(ctx, podKey, pod); err != nil {
				t.Fatalf("check pod not in scratch namespace: %v", err)
			}

			if pod.Spec.Volumes[0].PersistentVolumeClaim.ClaimName != testDataName {
				t.Fatalf("pod mounts %v, want restored PVC data", pod.Spec.Volumes[0])
			}

			pod.Status.Phase = tt.podPhase
			if err := c.Status().Update(ctx, pod); err != nil {
				t.Fatal(err)
			}

			reconcileRestore(t, r, key)

			if got := getRestore(t, c, key).Status.Phase; got != tt.wantPhase {
				t.Fatalf("phase %q, want %q", got, tt.wantPhase)
			}

			err := c.Get(ctx, client.ObjectKeyFromObject(scratch), scratch)
			if tt.wantNS != (err == nil) {
				t.Fatalf("scratch namespace present=%v, want %v (%v)", err == nil, tt.wantNS, err)
			}

			if tt.wantNS && scratch.Labels[retainedLabel] != testTrue {
				t.Fatalf("kept namespace not labelled retained: %v", scratch.Labels)
			}

			// Velero restore is cleaned the same way, and a second cleanup is a no-op.
			if err := r.cleanup(ctx, getRestore(t, c, key)); err != nil {
				t.Fatalf("second cleanup: %v", err)
			}

			err = c.Get(ctx, client.ObjectKeyFromObject(vr), r.veleroStub(restore))
			if tt.wantNS == errors.IsNotFound(err) {
				t.Fatalf("velero restore presence wrong, keep=%v err=%v", tt.keep, err)
			}
		})
	}
}

func TestVeleroRejectsRestoreNamespaceReferences(t *testing.T) {
	tests := []struct {
		name   string
		mutate func(*restorev1alpha1.RestoreCheck)
		field  string
	}{
		{"service account", func(bp *restorev1alpha1.RestoreCheck) { bp.Spec.ServiceAccountName = "sa" }, "serviceAccountName"},
		{"configmap volume", func(bp *restorev1alpha1.RestoreCheck) {
			bp.Spec.Volumes = []corev1.Volume{{Name: "v", VolumeSource: corev1.VolumeSource{ConfigMap: &corev1.ConfigMapVolumeSource{}}}}
		}, testVolumes},
		{"secret volume", func(bp *restorev1alpha1.RestoreCheck) {
			bp.Spec.Volumes = []corev1.Volume{{Name: "v", VolumeSource: corev1.VolumeSource{Secret: &corev1.SecretVolumeSource{}}}}
		}, testVolumes},
		{"projected volume", func(bp *restorev1alpha1.RestoreCheck) {
			bp.Spec.Volumes = []corev1.Volume{{Name: "v", VolumeSource: corev1.VolumeSource{Projected: &corev1.ProjectedVolumeSource{}}}}
		}, testVolumes},
		{"pvc volume", func(bp *restorev1alpha1.RestoreCheck) {
			bp.Spec.Volumes = []corev1.Volume{{Name: "v", VolumeSource: corev1.VolumeSource{
				PersistentVolumeClaim: &corev1.PersistentVolumeClaimVolumeSource{ClaimName: "c"},
			}}}
		}, testVolumes},
		{"clean", func(*restorev1alpha1.RestoreCheck) {}, ""},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()

			restore := newTestRestore()
			restore.Spec.Provider = restorev1alpha1.ProviderVelero

			bp := newTestCheck()
			tt.mutate(bp)

			c, r := fakeRestoreClient(t, restore, bp)
			key := client.ObjectKeyFromObject(restore)

			reconcileRestore(t, r, key) // Pending
			reconcileRestore(t, r, key)

			got := getRestore(t, c, key)
			err := c.Get(ctx, client.ObjectKeyFromObject(r.veleroStub(restore)), r.veleroStub(restore))

			if tt.field == "" {
				if err != nil || got.Status.Phase != restorev1alpha1.RestorePhaseRunning {
					t.Fatalf("clean check: phase %q, velero restore err %v", got.Status.Phase, err)
				}

				return
			}

			if got.Status.Phase != restorev1alpha1.RestorePhaseFailed || readyReason(got) != reasonProviderUnsupported ||
				!strings.Contains(got.Status.Message, tt.field) {
				t.Fatalf("phase %q reason %q message %q", got.Status.Phase, readyReason(got), got.Status.Message)
			}

			if !errors.IsNotFound(err) {
				t.Fatalf("velero restore created for rejected check: %v", err)
			}
		})
	}
}

func TestVeleroRejectsHostPath(t *testing.T) {
	bp := newTestCheck()
	bp.Spec.Volumes = []corev1.Volume{{Name: "v", VolumeSource: corev1.VolumeSource{HostPath: &corev1.HostPathVolumeSource{Path: "/"}}}}

	if err := validateVeleroCheck(bp); err == nil || !strings.Contains(err.Error(), "hostPath") {
		t.Fatalf("err = %v, want a hostPath rejection", err)
	}
}

const (
	testPullSecret = "pull"
	testConfigMap  = "conf"
)

// refCheck reads a pull secret, an env secret, an optional env secret and an envFrom configMap.
func refCheck() *restorev1alpha1.RestoreCheck {
	bp := veleroTwoVolumeCheck()
	bp.Spec.ImagePullSecrets = []corev1.LocalObjectReference{{Name: testPullSecret}}
	bp.Spec.Containers[0].Env = []corev1.EnvVar{
		{Name: "P", ValueFrom: &corev1.EnvVarSource{SecretKeyRef: &corev1.SecretKeySelector{
			LocalObjectReference: corev1.LocalObjectReference{Name: "db"}, Key: "password",
		}}},
		{Name: "O", ValueFrom: &corev1.EnvVarSource{SecretKeyRef: &corev1.SecretKeySelector{
			LocalObjectReference: corev1.LocalObjectReference{Name: "absent"}, Key: "k", Optional: boolPtr(true),
		}}},
	}
	bp.Spec.Containers[0].EnvFrom = []corev1.EnvFromSource{
		{ConfigMapRef: &corev1.ConfigMapEnvSource{LocalObjectReference: corev1.LocalObjectReference{Name: testConfigMap}}},
	}

	return bp
}

func TestVeleroAcceptsSecretAndConfigMapRefs(t *testing.T) {
	if err := validateVeleroCheck(refCheck()); err != nil {
		t.Fatalf("unexpected err = %v", err)
	}
}

func TestVeleroCopiesCheckRefs(t *testing.T) {
	ctx := context.Background()
	c, r, restore := completedVelero(t, refCheck(), htmlName, testPGData)
	key := client.ObjectKeyFromObject(restore)

	srcs := []client.Object{
		&corev1.Secret{ObjectMeta: metav1.ObjectMeta{Name: testPullSecret, Namespace: testNamespace}, Type: corev1.SecretTypeDockerConfigJson,
			Data: map[string][]byte{corev1.DockerConfigJsonKey: []byte("{}")}},
		&corev1.Secret{ObjectMeta: metav1.ObjectMeta{Name: "db", Namespace: testNamespace}, Data: map[string][]byte{"password": []byte("pw")}},
		&corev1.ConfigMap{ObjectMeta: metav1.ObjectMeta{Name: testConfigMap, Namespace: testNamespace}, Data: map[string]string{"A": "b"}},
	}
	for _, o := range srcs {
		if err := c.Create(ctx, o); err != nil {
			t.Fatal(err)
		}
	}

	reconcileRestore(t, r, key)

	scratch := veleroScratchName(restore)

	pull := &corev1.Secret{}
	if err := c.Get(ctx, client.ObjectKey{Name: testPullSecret, Namespace: scratch}, pull); err != nil {
		t.Fatal(err)
	}

	if pull.Type != corev1.SecretTypeDockerConfigJson || pull.Labels[restoreLabel] != restore.Name {
		t.Fatalf("pull secret copy: type %q labels %v", pull.Type, pull.Labels)
	}

	db := &corev1.Secret{}
	if err := c.Get(ctx, client.ObjectKey{Name: "db", Namespace: scratch}, db); err != nil || string(db.Data["password"]) != "pw" {
		t.Fatalf("db secret copy: %v %v", err, db.Data)
	}

	conf := &corev1.ConfigMap{}
	if err := c.Get(ctx, client.ObjectKey{Name: testConfigMap, Namespace: scratch}, conf); err != nil || conf.Data["A"] != "b" {
		t.Fatalf("configMap copy: %v %v", err, conf.Data)
	}

	pod := &corev1.Pod{}
	if err := c.Get(ctx, client.ObjectKey{Name: checkPodName(restore), Namespace: scratch}, pod); err != nil {
		t.Fatalf("check pod not created after the optional secret was skipped: %v", err)
	}
}

func TestVeleroMissingCheckRefFails(t *testing.T) {
	c, r, restore := completedVelero(t, refCheck(), htmlName, testPGData)
	key := client.ObjectKeyFromObject(restore)

	res, err := r.Reconcile(context.Background(), reconcile.Request{NamespacedName: key})
	if err != nil {
		t.Fatal(err)
	}

	got := getRestore(t, c, key)
	if got.Status.Phase != restorev1alpha1.RestorePhaseFailed || readyReason(got) != reasonCheckRefMissing ||
		!strings.Contains(got.Status.Message, "secret pull") {
		t.Fatalf("phase %q reason %q message %q", got.Status.Phase, readyReason(got), got.Status.Message)
	}

	if res.RequeueAfter != 0 {
		t.Fatalf("failed restore requeued: %v", res)
	}

	err = c.Get(context.Background(), client.ObjectKey{Name: checkPodName(restore), Namespace: veleroScratchName(restore)}, &corev1.Pod{})
	if !errors.IsNotFound(err) {
		t.Fatalf("check pod created without its secret: %v", err)
	}
}

func veleroSelectorRestore() *restorev1alpha1.Restore {
	return sizeTestRestore("velero")
}

func TestVeleroCopiesLabelSelector(t *testing.T) {
	restore := veleroSelectorRestore()
	restore.Spec.Velero = &restorev1alpha1.VeleroOptions{LabelSelector: &metav1.LabelSelector{MatchLabels: map[string]string{testLabelKey: "db"}}}

	obj := (&RestoreReconciler{}).veleroRestoreObject(restore)

	got, _, _ := unstructured.NestedMap(obj.Object, "spec", "labelSelector")
	if want := map[string]any{testMatchLabels: map[string]any{testLabelKey: "db"}}; !reflect.DeepEqual(got, want) {
		t.Fatalf("labelSelector = %v, want %v", got, want)
	}

	if _, found, _ := unstructured.NestedSlice(obj.Object, "spec", "orLabelSelectors"); found {
		t.Fatal("orLabelSelectors set without being asked")
	}
}

func TestVeleroCopiesOrLabelSelectors(t *testing.T) {
	restore := veleroSelectorRestore()
	restore.Spec.Velero = &restorev1alpha1.VeleroOptions{OrLabelSelectors: []*metav1.LabelSelector{
		{MatchLabels: map[string]string{testLabelKey: "db"}},
		{MatchLabels: map[string]string{testLabelKey: "web"}},
	}}

	obj := (&RestoreReconciler{}).veleroRestoreObject(restore)

	got, _, _ := unstructured.NestedSlice(obj.Object, "spec", "orLabelSelectors")
	want := []any{
		map[string]any{testMatchLabels: map[string]any{testLabelKey: "db"}},
		map[string]any{testMatchLabels: map[string]any{testLabelKey: "web"}},
	}

	if !reflect.DeepEqual(got, want) {
		t.Fatalf("orLabelSelectors = %v, want %v", got, want)
	}

	if _, found, _ := unstructured.NestedMap(obj.Object, "spec", "labelSelector"); found {
		t.Fatal("labelSelector set without being asked")
	}
}

// completedVelero reconciles a Velero Restore to Completed and creates the scratch namespace with the named PVCs.
func completedVelero(t *testing.T, bp *restorev1alpha1.RestoreCheck, pvcs ...string) (client.Client, *RestoreReconciler, *restorev1alpha1.Restore) {
	t.Helper()

	ctx := context.Background()
	restore := newTestRestore()
	restore.Spec.Provider = restorev1alpha1.ProviderVelero
	restore.Spec.Sources = nil

	// One Velero Backup holds every PVC, so all sources share the backupRef.
	for _, rv := range bp.Spec.RestoredVolumes {
		restore.Spec.Sources = append(restore.Spec.Sources, restorev1alpha1.RestoreSource{
			SourcePVC: rv.SourcePVC,
			BackupRef: restorev1alpha1.BackupRef{Name: "b1"},
		})
	}

	c, r := fakeRestoreClient(t, restore, bp, &corev1.Namespace{ObjectMeta: metav1.ObjectMeta{Name: testNamespace}})
	key := client.ObjectKeyFromObject(restore)

	reconcileRestore(t, r, key) // Pending
	reconcileRestore(t, r, key) // Velero Restore created

	vr := r.veleroStub(restore)
	if err := c.Get(ctx, client.ObjectKeyFromObject(vr), vr); err != nil {
		t.Fatal(err)
	}

	if err := unstructured.SetNestedField(vr.Object, "Completed", "status", "phase"); err != nil {
		t.Fatal(err)
	}

	if err := unstructured.SetNestedField(vr.Object, int64(1), "status", "progress", "itemsRestored"); err != nil {
		t.Fatal(err)
	}

	if err := c.Update(ctx, vr); err != nil {
		t.Fatal(err)
	}

	scratch := veleroScratchNamespace(restore)
	if err := c.Create(ctx, scratch); err != nil {
		t.Fatal(err)
	}

	for _, n := range pvcs {
		if err := c.Create(ctx, &corev1.PersistentVolumeClaim{ObjectMeta: metav1.ObjectMeta{Name: n, Namespace: scratch.Name}}); err != nil {
			t.Fatal(err)
		}
	}

	return c, r, restore
}

func veleroTwoVolumeCheck() *restorev1alpha1.RestoreCheck {
	bp := newTestCheck()
	bp.Spec.RestoredVolumes = []restorev1alpha1.RestoredVolume{
		{SourcePVC: htmlName, MountPath: "/html"},
		{SourcePVC: testPGData, MountPath: "/pgdata"},
	}
	bp.Spec.ScratchVolumes = []restorev1alpha1.ScratchVolume{{Name: "work", MountPath: "/work"}}

	return bp
}

func TestVeleroTwoClaimsMounted(t *testing.T) {
	c, r, restore := completedVelero(t, veleroTwoVolumeCheck(), htmlName, testPGData)
	key := client.ObjectKeyFromObject(restore)

	reconcileRestore(t, r, key)

	pod := &corev1.Pod{}
	if err := c.Get(context.Background(), client.ObjectKey{Name: checkPodName(restore), Namespace: veleroScratchName(restore)}, pod); err != nil {
		t.Fatal(err)
	}

	var claims []string

	for _, v := range pod.Spec.Volumes {
		if v.PersistentVolumeClaim != nil && !strings.HasPrefix(v.Name, "scratch-") {
			claims = append(claims, v.PersistentVolumeClaim.ClaimName)
		}
	}

	if !reflect.DeepEqual(claims, []string{htmlName, testPGData}) {
		t.Fatalf("pod mounts %v, want html and pgdata", claims)
	}
}

func TestVeleroMissingClaimFails(t *testing.T) {
	c, r, restore := completedVelero(t, veleroTwoVolumeCheck(), htmlName)
	key := client.ObjectKeyFromObject(restore)

	res, err := r.Reconcile(context.Background(), reconcile.Request{NamespacedName: key})
	if err != nil {
		t.Fatal(err)
	}

	got := getRestore(t, c, key)
	if got.Status.Phase != restorev1alpha1.RestorePhaseFailed || readyReason(got) != reasonRestoredVolumeMissing ||
		!strings.Contains(got.Status.Message, testPGData) || strings.Contains(got.Status.Message, htmlName) {
		t.Fatalf("phase %q reason %q message %q", got.Status.Phase, readyReason(got), got.Status.Message)
	}

	if res.RequeueAfter != 0 {
		t.Fatalf("failed restore requeued: %v", res)
	}
}

// listFrom reads like Reader, but lists from another reader.
type listFrom struct {
	client.Reader
	list client.Reader
}

func (l listFrom) List(ctx context.Context, list client.ObjectList, opts ...client.ListOption) error {
	return l.list.List(ctx, list, opts...)
}

func TestVeleroClaimsUseAPIReader(t *testing.T) {
	c, r, restore := completedVelero(t, veleroTwoVolumeCheck())
	ctx := context.Background()

	// The claims exist only for the API reader. The cache client never sees them.
	other, _ := fakeRestoreClient(t)

	for _, n := range []string{htmlName, testPGData} {
		if err := other.Create(ctx, &corev1.PersistentVolumeClaim{ObjectMeta: metav1.ObjectMeta{Name: n, Namespace: veleroScratchName(restore)}}); err != nil {
			t.Fatal(err)
		}
	}

	r.APIReader = listFrom{Reader: c, list: other}

	reconcileRestore(t, r, client.ObjectKeyFromObject(restore))

	if err := c.Get(ctx, client.ObjectKey{Name: checkPodName(restore), Namespace: veleroScratchName(restore)}, &corev1.Pod{}); err != nil {
		t.Fatalf("check pod not created from API reader claims: %v", err)
	}
}

func TestOwnsScratch(t *testing.T) {
	ctx := context.Background()
	restore := newTestRestore()
	restore.Spec.Provider = restorev1alpha1.ProviderVelero

	c, r := fakeRestoreClient(t, restore, veleroScratchNamespace(restore))

	if owned, err := r.ownsScratch(ctx, restore); err != nil || owned {
		t.Fatalf("unlabeled namespace without a Velero Restore: owned %v, %v, want false", owned, err)
	}

	if err := c.Create(ctx, r.veleroRestoreObject(restore)); err != nil {
		t.Fatal(err)
	}

	if owned, err := r.ownsScratch(ctx, restore); err != nil || !owned {
		t.Fatalf("namespace of our Velero Restore: owned %v, %v, want true", owned, err)
	}
}

func TestCopyPodSecurityLabelsScratch(t *testing.T) {
	ctx := context.Background()
	restore := newTestRestore()
	scratch := veleroScratchNamespace(restore)

	c, r := fakeRestoreClient(t, restore, scratch, &corev1.Namespace{ObjectMeta: metav1.ObjectMeta{Name: testNamespace}})

	if err := r.copyPodSecurity(ctx, restore, scratch); err != nil {
		t.Fatal(err)
	}

	stored := &corev1.Namespace{}
	if err := c.Get(ctx, client.ObjectKeyFromObject(scratch), stored); err != nil {
		t.Fatal(err)
	}

	if stored.Labels[restoreLabel] != restore.Name {
		t.Fatalf("labels = %v, want %s=%s", stored.Labels, restoreLabel, restore.Name)
	}
}
