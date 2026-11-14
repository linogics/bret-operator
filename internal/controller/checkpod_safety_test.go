package controller

import (
	"testing"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	restorev1alpha1 "github.com/linogics/bret-operator/api/v1alpha1"
)

// bareCheck is a check that sets no security fields, so the operator defaults apply.
func bareCheck() *restorev1alpha1.RestoreCheck {
	return &restorev1alpha1.RestoreCheck{
		ObjectMeta: metav1.ObjectMeta{Name: testApp, Namespace: testNamespace},
		Spec: restorev1alpha1.RestoreCheckSpec{
			RestoredVolumes: []restorev1alpha1.RestoredVolume{{SourcePVC: testDataName, MountPath: testMountPath}},
			Containers:      []corev1.Container{{Name: testCheckName, Image: testImage}},
		},
	}
}

func buildBare(t *testing.T, bp *restorev1alpha1.RestoreCheck) *corev1.Pod {
	t.Helper()

	pod, err := buildCheckPod(newTestRestore(), bp, "r1-scratch", nil)
	if err != nil {
		t.Fatalf("buildCheckPod: %v", err)
	}

	return pod
}

func TestCheckPodDefaultsAreSafe(t *testing.T) {
	t.Parallel()

	pod := buildBare(t, bareCheck())

	if pod.Spec.AutomountServiceAccountToken == nil || *pod.Spec.AutomountServiceAccountToken {
		t.Errorf("automountServiceAccountToken must default to false")
	}

	if sp := pod.Spec.SecurityContext.SeccompProfile; sp == nil || sp.Type != corev1.SeccompProfileTypeRuntimeDefault {
		t.Errorf("seccompProfile must default to RuntimeDefault, got %v", sp)
	}

	sc := pod.Spec.Containers[0].SecurityContext
	if sc == nil || sc.AllowPrivilegeEscalation == nil || *sc.AllowPrivilegeEscalation {
		t.Errorf("allowPrivilegeEscalation must default to false")
	}

	if sc == nil || sc.Privileged == nil || *sc.Privileged {
		t.Errorf("privileged must default to false")
	}
}

func TestCheckPodExplicitValuesWin(t *testing.T) {
	t.Parallel()

	bp := bareCheck()
	yes := true
	uid := int64(1000)

	bp.Spec.AutomountServiceAccountToken = &yes
	bp.Spec.Containers[0].SecurityContext = &corev1.SecurityContext{
		Privileged:               &yes,
		AllowPrivilegeEscalation: &yes,
		RunAsUser:                &uid,
	}

	pod := buildBare(t, bp)

	if !*pod.Spec.AutomountServiceAccountToken {
		t.Errorf("explicit automountServiceAccountToken: true was overwritten")
	}

	sc := pod.Spec.Containers[0].SecurityContext
	if !*sc.Privileged || !*sc.AllowPrivilegeEscalation {
		t.Errorf("explicit privileged or allowPrivilegeEscalation was overwritten")
	}

	if sc.RunAsUser == nil || *sc.RunAsUser != uid {
		t.Errorf("runAsUser was lost")
	}
}

func TestCheckRejectsHostPathUnlessAllowed(t *testing.T) {
	t.Parallel()

	bp := bareCheck()
	bp.Spec.Volumes = []corev1.Volume{{
		Name:         "host",
		VolumeSource: corev1.VolumeSource{HostPath: &corev1.HostPathVolumeSource{Path: "/"}},
	}}

	if err := validateCheck(bp); err == nil {
		t.Fatalf("hostPath must be rejected by default")
	}

	bp.Annotations = map[string]string{allowHostPathAnnotation: testTrue}

	if err := validateCheck(bp); err != nil {
		t.Fatalf("hostPath with the opt-in annotation must pass: %v", err)
	}
}

func TestRenderCheckMatchesOperatorPod(t *testing.T) {
	t.Parallel()

	bp := bareCheck()
	bp.Spec.ScratchVolumes = []restorev1alpha1.ScratchVolume{
		{Name: testWorkArea, MountPath: testScratchPath, Size: resource.MustParse("1Gi")},
	}

	objs, err := RenderCheck(newTestRestore(), bp, "r1-scratch")
	if err != nil {
		t.Fatalf("RenderCheck: %v", err)
	}

	if len(objs) != 2 {
		t.Fatalf("want one scratch PVC and one pod, got %d objects", len(objs))
	}

	pod, ok := objs[len(objs)-1].(*corev1.Pod)
	if !ok {
		t.Fatalf("last object is %T, want *corev1.Pod", objs[len(objs)-1])
	}

	if pod.Spec.AutomountServiceAccountToken == nil || *pod.Spec.AutomountServiceAccountToken {
		t.Errorf("rendered pod is not hardened")
	}
}
