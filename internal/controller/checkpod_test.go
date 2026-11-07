package controller

import (
	"context"
	"testing"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"

	restorev1alpha1 "github.com/linogics/bret-operator/api/v1alpha1"
)

func checkPodBlueprint() *restorev1alpha1.RestoreBlueprint {
	uid := int64(999)

	return &restorev1alpha1.RestoreBlueprint{
		ObjectMeta: metav1.ObjectMeta{Name: testApp, Namespace: testNamespace},
		Spec: restorev1alpha1.RestoreBlueprintSpec{
			RestoreMount: testMountPath,
			InitContainers: []corev1.Container{
				{Name: "perms", Image: "busybox"},
			},
			Containers: []corev1.Container{
				{
					Name:            testCheckName,
					Image:           "postgres:17",
					SecurityContext: &corev1.SecurityContext{RunAsUser: &uid},
				},
			},
			ScratchVolumes: []restorev1alpha1.ScratchVolume{
				{Name: "Work Area", MountPath: "/scratch", Size: resource.MustParse("1Gi")},
			},
			ServiceAccountName: "checker",
			NodeSelector:       map[string]string{"disk": "ssd"},
			PriorityClassName:  "high",
		},
	}
}

func TestBuildCheckPod(t *testing.T) {
	t.Parallel()

	restore := newTestRestore()
	bp := checkPodBlueprint()

	pod, err := buildCheckPod(restore, bp, "r1-scratch", map[string]string{"Work Area": "r1-scratch-work-area"})
	if err != nil {
		t.Fatalf("buildCheckPod: %v", err)
	}

	if pod.Name != "r1-check" || pod.Namespace != testNamespace {
		t.Errorf("pod name %s/%s, want default/r1-check", pod.Namespace, pod.Name)
	}

	if pod.Spec.RestartPolicy != corev1.RestartPolicyNever {
		t.Errorf("restartPolicy %q, want Never", pod.Spec.RestartPolicy)
	}

	if pod.Labels[restoreLabel] != "r1" {
		t.Errorf("label %s = %q, want r1", restoreLabel, pod.Labels[restoreLabel])
	}

	if pod.Spec.ServiceAccountName != "checker" || pod.Spec.PriorityClassName != "high" || pod.Spec.NodeSelector["disk"] != "ssd" {
		t.Errorf("pod-level fields not copied from blueprint: %+v", pod.Spec)
	}

	if got := pvcClaim(pod, restoredVolume); got != "r1-scratch" {
		t.Errorf("restored volume claim %q, want r1-scratch", got)
	}

	if got := pvcClaim(pod, "scratch-work-area"); got != "r1-scratch-work-area" {
		t.Errorf("scratch volume claim %q, want r1-scratch-work-area", got)
	}

	for _, c := range append(append([]corev1.Container{}, pod.Spec.InitContainers...), pod.Spec.Containers...) {
		mounts := map[string]string{}
		for _, m := range c.VolumeMounts {
			mounts[m.Name] = m.MountPath
		}

		if mounts[restoredVolume] != testMountPath {
			t.Errorf("container %s: restored mount %q, want /var/lib/postgresql/data", c.Name, mounts[restoredVolume])
		}

		if mounts["scratch-work-area"] != "/scratch" {
			t.Errorf("container %s: scratch mount %q, want /scratch", c.Name, mounts["scratch-work-area"])
		}
	}

	if len(bp.Spec.InitContainers[0].VolumeMounts) != 0 {
		t.Errorf("buildCheckPod changed the blueprint's init container mounts")
	}
}

func TestBuildCheckPodRejectsMissingScratchClaim(t *testing.T) {
	t.Parallel()

	if _, err := buildCheckPod(newTestRestore(), checkPodBlueprint(), "r1-scratch", nil); err == nil {
		t.Error("missing scratch claim: want error, got none")
	}
}

func TestRestoredVolumesMapping(t *testing.T) {
	t.Parallel()

	bp := checkPodBlueprint()
	bp.Spec.RestoreMount = ""
	bp.Spec.RestoredVolumes = []restorev1alpha1.RestoredVolume{{Name: "data", MountPath: "/data", SourcePVC: "pg-data"}}

	pod, err := buildCheckPod(newTestRestore(), bp, "r1-scratch", map[string]string{"Work Area": "r1-scratch-work-area"})
	if err != nil {
		t.Fatalf("buildCheckPod: %v", err)
	}

	if got := pvcClaim(pod, "data"); got != "r1-scratch" {
		t.Errorf("volume data claim %q, want r1-scratch", got)
	}

	for _, c := range pod.Spec.Containers {
		mounts := map[string]string{}
		for _, m := range c.VolumeMounts {
			mounts[m.Name] = m.MountPath
		}

		if mounts["data"] != "/data" {
			t.Errorf("container %s: data mount %q, want /data", c.Name, mounts["data"])
		}
	}
}

func TestRestoredVolumesRejectsMoreThanOne(t *testing.T) {
	t.Parallel()

	bp := checkPodBlueprint()
	bp.Spec.RestoreMount = ""
	bp.Spec.RestoredVolumes = []restorev1alpha1.RestoredVolume{
		{Name: "a", MountPath: "/a", SourcePVC: "pa"},
		{Name: "b", MountPath: "/b", SourcePVC: "pb"},
	}

	if err := validateRestoredVolumes(bp); err == nil {
		t.Error("two restoredVolumes: want error, got none")
	}

	c, r := fakeRestoreClient(t, newTestRestore(), bp)
	key := types.NamespacedName{Namespace: testNamespace, Name: "r1"}

	reconcileRestore(t, r, key)
	reconcileRestore(t, r, key)

	got := getRestore(t, c, key)
	if readyReason(got) != reasonProviderUnsupported || got.Status.Phase != restorev1alpha1.RestorePhaseFailed {
		t.Errorf("phase %q reason %q, want Failed/ProviderUnsupported", got.Status.Phase, readyReason(got))
	}

	pvcs := &corev1.PersistentVolumeClaimList{}
	if err := c.List(context.Background(), pvcs); err != nil || len(pvcs.Items) != 0 {
		t.Errorf("PVCs created for rejected restore: %d, err %v", len(pvcs.Items), err)
	}
}

func TestMoverUID(t *testing.T) {
	t.Parallel()

	bp := checkPodBlueprint()

	uid, err := moverUID(bp)
	if err != nil || uid == nil || *uid != 999 {
		t.Fatalf("container runAsUser: got %v, %v; want 999", uid, err)
	}

	bp.Annotations = map[string]string{moverUIDAnnotation: `{"data": 70}`}

	uid, err = moverUID(bp)
	if err != nil || uid == nil || *uid != 70 {
		t.Fatalf("annotation: got %v, %v; want 70", uid, err)
	}
}

func pvcClaim(pod *corev1.Pod, volume string) string {
	for _, v := range pod.Spec.Volumes {
		if v.Name == volume && v.PersistentVolumeClaim != nil {
			return v.PersistentVolumeClaim.ClaimName
		}
	}

	return ""
}
