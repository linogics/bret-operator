package controller

import (
	"fmt"
	"regexp"
	"strings"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/util/validation"
	"sigs.k8s.io/controller-runtime/pkg/client"

	restorev1alpha1 "github.com/linogics/bret-operator/api/v1alpha1"
)

const (
	restoreLabel = "bret.linogics.io/restore"
	checkLabel   = "bret.linogics.io/check"
)

var nonDNSLabelChars = regexp.MustCompile(`[^a-z0-9-]`)

// dnsLabel turns s into a DNS label: lower case, only a-z, 0-9 and "-", at most 50 characters, no leading or trailing dash.
func dnsLabel(s string) string {
	s = nonDNSLabelChars.ReplaceAllString(strings.ToLower(s), "-")
	s = strings.Trim(s, "-")

	if len(s) > 50 {
		s = strings.Trim(s[:50], "-")
	}

	return s
}

// validateCheck rejects checks that this round cannot run.
func validateCheck(bp *restorev1alpha1.RestoreCheck) error {
	if len(bp.Spec.RestoredVolumes) == 0 {
		return fmt.Errorf("check %s: restoredVolumes is required", bp.Name)
	}

	seen := map[string]bool{}

	for _, rv := range bp.Spec.RestoredVolumes {
		if errs := validation.IsDNS1123Label(rv.SourcePVC); len(errs) > 0 {
			return fmt.Errorf("check %s: sourcePVC %q is not a DNS label: %s", bp.Name, rv.SourcePVC, errs[0])
		}

		if seen[rv.SourcePVC] {
			return fmt.Errorf("check %s: sourcePVC %q is used twice", bp.Name, rv.SourcePVC)
		}

		seen[rv.SourcePVC] = true

		for _, sv := range bp.Spec.ScratchVolumes {
			if rv.SourcePVC == "scratch-"+dnsLabel(sv.Name) {
				return fmt.Errorf("check %s: sourcePVC %q clashes with the claim of scratch volume %q", bp.Name, rv.SourcePVC, sv.Name)
			}
		}
	}

	mounts := map[string]bool{}

	paths := make([]string, 0, len(bp.Spec.RestoredVolumes)+len(bp.Spec.ScratchVolumes))
	for _, v := range bp.Spec.RestoredVolumes {
		paths = append(paths, v.MountPath)
	}

	for _, v := range bp.Spec.ScratchVolumes {
		paths = append(paths, v.MountPath)
	}

	for _, p := range paths {
		if mounts[p] {
			return fmt.Errorf("check %s: mountPath %q is used twice", bp.Name, p)
		}

		mounts[p] = true
	}

	if bp.Annotations[allowHostPathAnnotation] == "true" {
		return nil
	}

	for _, v := range bp.Spec.Volumes {
		if v.HostPath != nil {
			return fmt.Errorf("check %s: volume %q is a hostPath; set annotation %s=\"true\" to allow it",
				bp.Name, v.Name, allowHostPathAnnotation)
		}
	}

	return nil
}

// checkPodName is the name of the check pod of one restore attempt.
func checkPodName(restore *restorev1alpha1.Restore) string {
	return restore.Name + "-check"
}

// buildCheckPod renders the check pod for one attempt.
// restoredClaims maps each restoredVolumes sourcePVC to its PVC, mounted at the entry's mountPath.
// scratchClaims maps scratch volume names to their PVC names.
func buildCheckPod(
	restore *restorev1alpha1.Restore,
	bp *restorev1alpha1.RestoreCheck,
	restoredClaims map[string]string,
	scratchClaims map[string]string,
) (*corev1.Pod, error) {
	err := validateCheck(bp)
	if err != nil {
		return nil, err
	}

	spec := bp.Spec.DeepCopy()

	volumes := make([]corev1.Volume, 0, len(spec.RestoredVolumes)+len(spec.ScratchVolumes))
	mounts := make([]corev1.VolumeMount, 0, cap(volumes))

	for _, rv := range spec.RestoredVolumes {
		claim, ok := restoredClaims[rv.SourcePVC]
		if !ok {
			return nil, fmt.Errorf("no claim for restored volume %q", rv.SourcePVC)
		}

		name := restoredVolumeName(rv.SourcePVC)
		volumes = append(volumes, pvcVolume(name, claim))
		mounts = append(mounts, corev1.VolumeMount{Name: name, MountPath: rv.MountPath})
	}

	for _, sv := range spec.ScratchVolumes {
		claim, ok := scratchClaims[sv.Name]
		if !ok {
			return nil, fmt.Errorf("no claim for scratch volume %q", sv.Name)
		}

		name := "scratch-" + dnsLabel(sv.Name)
		volumes = append(volumes, pvcVolume(name, claim))
		mounts = append(mounts, corev1.VolumeMount{Name: name, MountPath: sv.MountPath})
	}

	volumes = append(volumes, spec.Volumes...)

	initContainers := withMounts(spec.InitContainers, mounts)
	containers := withMounts(spec.Containers, mounts)

	for i := range initContainers {
		hardenContainer(&initContainers[i])
	}

	for i := range containers {
		hardenContainer(&containers[i])
	}

	return &corev1.Pod{
		ObjectMeta: metav1.ObjectMeta{
			Name:      checkPodName(restore),
			Namespace: restore.Namespace,
			Labels: map[string]string{
				restoreLabel: restore.Name,
				checkLabel:   dnsLabel(bp.Name),
			},
		},
		Spec: corev1.PodSpec{
			RestartPolicy:                corev1.RestartPolicyNever,
			InitContainers:               initContainers,
			Containers:                   containers,
			Volumes:                      volumes,
			SecurityContext:              hardenPod(spec.SecurityContext),
			AutomountServiceAccountToken: automountToken(bp),
			ServiceAccountName:           spec.ServiceAccountName,
			ImagePullSecrets:             spec.ImagePullSecrets,
			NodeSelector:                 spec.NodeSelector,
			Tolerations:                  spec.Tolerations,
			Affinity:                     spec.Affinity,
			PriorityClassName:            spec.PriorityClassName,
			RuntimeClassName:             spec.RuntimeClassName,
		},
	}, nil
}

// withMounts returns copies of containers with appended mounts. It leaves the check untouched.
func withMounts(containers []corev1.Container, mounts []corev1.VolumeMount) []corev1.Container {
	out := make([]corev1.Container, 0, len(containers))

	for i := range containers {
		c := containers[i].DeepCopy()
		c.VolumeMounts = append(c.VolumeMounts, mounts...)
		out = append(out, *c)
	}

	return out
}

func pvcVolume(name, claim string) corev1.Volume {
	return corev1.Volume{
		Name: name,
		VolumeSource: corev1.VolumeSource{
			PersistentVolumeClaim: &corev1.PersistentVolumeClaimVolumeSource{ClaimName: claim},
		},
	}
}

// allowHostPathAnnotation lets a check mount a hostPath volume. Without it, validation rejects the check.
const allowHostPathAnnotation = "bret.linogics.io/allow-host-path"

// hardenPod sets the pod default: the RuntimeDefault seccomp profile. An explicit value wins.
func hardenPod(sc *corev1.PodSecurityContext) *corev1.PodSecurityContext {
	if sc == nil {
		sc = &corev1.PodSecurityContext{}
	}

	if sc.SeccompProfile == nil {
		sc.SeccompProfile = &corev1.SeccompProfile{Type: corev1.SeccompProfileTypeRuntimeDefault}
	}

	return sc
}

// hardenContainer sets the container defaults: no privilege escalation and not privileged.
// Explicit values win, so a check can still opt out of them.
func hardenContainer(c *corev1.Container) {
	if c.SecurityContext == nil {
		c.SecurityContext = &corev1.SecurityContext{}
	}

	if c.SecurityContext.AllowPrivilegeEscalation == nil {
		c.SecurityContext.AllowPrivilegeEscalation = boolPtr(false)
	}

	if c.SecurityContext.Privileged == nil {
		c.SecurityContext.Privileged = boolPtr(false)
	}
}

// automountToken is the check's service account token setting. Unset means false.
func automountToken(bp *restorev1alpha1.RestoreCheck) *bool {
	if bp.Spec.AutomountServiceAccountToken != nil {
		return bp.Spec.AutomountServiceAccountToken
	}

	return boolPtr(false)
}

func boolPtr(b bool) *bool {
	return &b
}

// RenderCheck returns the scratch PVCs and the check pod of one attempt, built the way the operator builds them.
// The check harness calls it, so the harness runs the same pod as the operator.
func RenderCheck(
	restore *restorev1alpha1.Restore,
	bp *restorev1alpha1.RestoreCheck,
	restoredClaims map[string]string,
) ([]client.Object, error) {
	var objs []client.Object

	scratch := map[string]string{}

	for _, sv := range bp.Spec.ScratchVolumes {
		pvc := scratchPVC(restore, sv)
		scratch[sv.Name] = pvc.Name
		objs = append(objs, pvc)
	}

	pod, err := buildCheckPod(restore, bp, restoredClaims, scratch)
	if err != nil {
		return nil, err
	}

	return append(objs, pod), nil
}
