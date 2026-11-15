package controller

import (
	"strings"
	"testing"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	restorev1alpha1 "github.com/linogics/bret-operator/api/v1alpha1"
)

func checkPodCheck() *restorev1alpha1.RestoreCheck {
	uid := int64(999)

	return &restorev1alpha1.RestoreCheck{
		ObjectMeta: metav1.ObjectMeta{Name: testApp, Namespace: testNamespace},
		Spec: restorev1alpha1.RestoreCheckSpec{
			RestoredVolumes: []restorev1alpha1.RestoredVolume{{SourcePVC: testDataName, MountPath: testMountPath}},
			InitContainers: []corev1.Container{
				{Name: "perms", Image: testImage},
			},
			Containers: []corev1.Container{
				{
					Name:            testCheckName,
					Image:           "postgres:17",
					SecurityContext: &corev1.SecurityContext{RunAsUser: &uid},
				},
			},
			ScratchVolumes: []restorev1alpha1.ScratchVolume{
				{Name: testWorkArea, MountPath: testScratchPath, Size: resource.MustParse("1Gi")},
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
	bp := checkPodCheck()

	pod, err := buildCheckPod(restore, bp, map[string]string{testDataName: testDataClaim}, map[string]string{testWorkArea: testWorkAreaClaim})
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
		t.Errorf("pod-level fields not copied from check: %+v", pod.Spec)
	}

	if got := pvcClaim(pod, restoredVolumeName(testDataName)); got != testDataClaim {
		t.Errorf("restored volume claim %q, want r1-data", got)
	}

	if got := pvcClaim(pod, "scratch-work-area"); got != testWorkAreaClaim {
		t.Errorf("scratch volume claim %q, want r1-scratch-work-area", got)
	}

	for _, c := range append(append([]corev1.Container{}, pod.Spec.InitContainers...), pod.Spec.Containers...) {
		mounts := map[string]string{}
		for _, m := range c.VolumeMounts {
			mounts[m.Name] = m.MountPath
		}

		if mounts[restoredVolumeName(testDataName)] != testMountPath {
			t.Errorf("container %s: restored mount %q, want /var/lib/postgresql/data", c.Name, mounts[restoredVolumeName(testDataName)])
		}

		if mounts["scratch-work-area"] != "/scratch" {
			t.Errorf("container %s: scratch mount %q, want /scratch", c.Name, mounts["scratch-work-area"])
		}
	}

	if len(bp.Spec.InitContainers[0].VolumeMounts) != 0 {
		t.Errorf("buildCheckPod changed the check's init container mounts")
	}
}

func TestBuildCheckPodRejectsMissingScratchClaim(t *testing.T) {
	t.Parallel()

	if _, err := buildCheckPod(newTestRestore(), checkPodCheck(), map[string]string{testDataName: testDataClaim}, nil); err == nil {
		t.Error("missing scratch claim: want error, got none")
	}
}

func TestRestoredVolumesMapping(t *testing.T) {
	t.Parallel()

	bp := checkPodCheck()
	bp.Spec.RestoredVolumes = []restorev1alpha1.RestoredVolume{{MountPath: testDataPath, SourcePVC: "pg-data"}}

	pod, err := buildCheckPod(newTestRestore(), bp, map[string]string{"pg-data": testDataClaim}, map[string]string{testWorkArea: testWorkAreaClaim})
	if err != nil {
		t.Fatalf("buildCheckPod: %v", err)
	}

	if got := pvcClaim(pod, restoredVolumeName("pg-data")); got != testDataClaim {
		t.Errorf("volume claim %q, want r1-data", got)
	}

	for _, c := range pod.Spec.Containers {
		mounts := map[string]string{}
		for _, m := range c.VolumeMounts {
			mounts[m.Name] = m.MountPath
		}

		if mounts[restoredVolumeName("pg-data")] != "/data" {
			t.Errorf("container %s: data mount %q, want /data", c.Name, mounts[restoredVolumeName("pg-data")])
		}
	}
}

func TestMoverUID(t *testing.T) {
	t.Parallel()

	bp := checkPodCheck()

	uid, err := MoverUID(bp, testDataName)
	if err != nil || uid == nil || *uid != 999 {
		t.Fatalf("container runAsUser: got %v, %v; want 999", uid, err)
	}

	bp.Annotations = map[string]string{moverUIDAnnotation: `{"data": 70}`}

	uid, err = MoverUID(bp, testDataName)
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

func TestDNSLabel(t *testing.T) {
	for _, in := range []string{strings.Repeat("a", 49) + "-b", "_", "-x-", "My_App"} {
		got := dnsLabel(in)

		if len(got) > 50 || strings.HasPrefix(got, "-") || strings.HasSuffix(got, "-") {
			t.Errorf("dnsLabel(%q) = %q, want at most 50 characters and no leading or trailing dash", in, got)
		}
	}
}

const (
	htmlName  = "html"
	htmlPath  = "/var/www/html"
	htmlClaim = "r1-html"
)

func twoVolumeCheck() *restorev1alpha1.RestoreCheck {
	bp := checkPodCheck()
	bp.Spec.RestoredVolumes = []restorev1alpha1.RestoredVolume{
		{SourcePVC: htmlName, MountPath: htmlPath},
		{SourcePVC: "pgdata", MountPath: "/var/lib/postgresql/data"},
	}

	return bp
}

func TestCheckPodMountsAllRestoredVolumes(t *testing.T) {
	t.Parallel()

	claims := map[string]string{htmlName: htmlClaim, "pgdata": "r1-pgdata"}

	pod, err := buildCheckPod(newTestRestore(), twoVolumeCheck(), claims, map[string]string{testWorkArea: testWorkAreaClaim})
	if err != nil {
		t.Fatalf("buildCheckPod: %v", err)
	}

	if got := pvcClaim(pod, "restored-html"); got != htmlClaim {
		t.Errorf("restored-html claim %q, want r1-html", got)
	}

	if got := pvcClaim(pod, "restored-pgdata"); got != "r1-pgdata" {
		t.Errorf("restored-pgdata claim %q, want r1-pgdata", got)
	}

	for _, c := range pod.Spec.Containers {
		mounts := map[string]string{}
		for _, m := range c.VolumeMounts {
			mounts[m.Name] = m.MountPath
		}

		if mounts["restored-html"] != htmlPath || mounts["restored-pgdata"] != "/var/lib/postgresql/data" {
			t.Errorf("container %s mounts %v, want both restored volumes", c.Name, mounts)
		}
	}
}

func TestCheckPodMissingRestoredClaim(t *testing.T) {
	t.Parallel()

	_, err := buildCheckPod(newTestRestore(), twoVolumeCheck(), map[string]string{htmlName: htmlClaim},
		map[string]string{testWorkArea: testWorkAreaClaim})
	if err == nil || !strings.Contains(err.Error(), "pgdata") {
		t.Errorf("err = %v, want one naming pgdata", err)
	}
}

func TestValidateCheckDuplicateMounts(t *testing.T) {
	t.Parallel()

	bp := twoVolumeCheck()
	bp.Spec.RestoredVolumes[1].MountPath = htmlPath

	if err := validateCheck(bp); err == nil {
		t.Error("two restored volumes on one mountPath: want error")
	}

	bp = twoVolumeCheck()
	bp.Spec.RestoredVolumes[0].MountPath = testScratchPath

	if err := validateCheck(bp); err == nil {
		t.Error("restored mountPath equal to a scratch mountPath: want error")
	}
}

func TestMoverUIDPerVolume(t *testing.T) {
	t.Parallel()

	bp := checkPodCheck()
	bp.Annotations = map[string]string{moverUIDAnnotation: `{"pgdata": 999}`}

	uid, err := MoverUID(bp, "pgdata")
	if err != nil || uid == nil || *uid != 999 {
		t.Fatalf("pgdata: got %v, %v; want 999", uid, err)
	}

	bp.Annotations = map[string]string{moverUIDAnnotation: `{"pgdata": 70}`}

	uid, err = MoverUID(bp, htmlName)
	if err != nil || uid == nil || *uid != 999 {
		t.Fatalf("html: got %v, %v; want container runAsUser 999", uid, err)
	}
}

func TestValidateCheckClaimClash(t *testing.T) {
	t.Parallel()

	bp := checkPodCheck()
	bp.Spec.RestoredVolumes[0].SourcePVC = "scratch-work-area"

	err := validateCheck(bp)
	if err == nil || !strings.Contains(err.Error(), "scratch-work-area") || !strings.Contains(err.Error(), testWorkArea) {
		t.Errorf("err = %v, want a clash naming both", err)
	}
}

func TestValidateCheckBadSourcePVC(t *testing.T) {
	t.Parallel()

	for _, name := range []string{"data.db", "Data"} {
		bp := checkPodCheck()
		bp.Spec.RestoredVolumes[0].SourcePVC = name

		if err := validateCheck(bp); err == nil {
			t.Errorf("sourcePVC %q: want error", name)
		}
	}
}

func TestValidateCheckDuplicateSourcePVC(t *testing.T) {
	t.Parallel()

	bp := twoVolumeCheck()
	bp.Spec.RestoredVolumes[1].SourcePVC = htmlName
	bp.Spec.RestoredVolumes[1].MountPath = "/other"

	if err := validateCheck(bp); err == nil {
		t.Error("duplicate sourcePVC: want error")
	}
}
