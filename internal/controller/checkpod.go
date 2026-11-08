package controller

import (
	"fmt"
	"regexp"
	"strings"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	restorev1alpha1 "github.com/linogics/bret-operator/api/v1alpha1"
)

const (
	restoreLabel   = "bret.linogics.io/restore"
	checkLabel     = "bret.linogics.io/check"
	restoredVolume = "restored-data"
)

var nonDNSLabelChars = regexp.MustCompile(`[^a-z0-9-]`)

// dnsLabel mirrors dns() in hack/check-test/render.py, so names match the rendered test pods.
func dnsLabel(s string) string {
	s = nonDNSLabelChars.ReplaceAllString(strings.ToLower(s), "-")
	s = strings.Trim(s, "-")

	if len(s) > 50 {
		s = s[:50]
	}

	return s
}

// validateCheck rejects checks that this round cannot run.
func validateCheck(bp *restorev1alpha1.RestoreCheck) error {
	if bp.Spec.RestoreMount == "" && len(bp.Spec.RestoredVolumes) == 0 {
		return fmt.Errorf("check %s: restoreMount or restoredVolumes is required", bp.Name)
	}

	return nil
}

// validateRestoredVolumes rejects checks with more than one restored volume: providers restore one PVC.
func validateRestoredVolumes(bp *restorev1alpha1.RestoreCheck) error {
	if len(bp.Spec.RestoredVolumes) > 1 {
		return fmt.Errorf("check %s: only one restoredVolumes entry is supported, got %d", bp.Name, len(bp.Spec.RestoredVolumes))
	}

	return nil
}

// checkPodName is the name of the check pod of one restore attempt.
func checkPodName(restore *restorev1alpha1.Restore) string {
	return restore.Name + "-check"
}

// buildCheckPod renders the check pod for one attempt, the same way hack/check-test/render.py does.
// restoredClaim is mounted at restoreMount; scratchClaims maps scratch volume names to their PVC names.
func buildCheckPod(
	restore *restorev1alpha1.Restore,
	bp *restorev1alpha1.RestoreCheck,
	restoredClaim string,
	scratchClaims map[string]string,
) (*corev1.Pod, error) {
	err := validateCheck(bp)
	if err == nil {
		err = validateRestoredVolumes(bp)
	}

	if err != nil {
		return nil, err
	}

	spec := bp.Spec.DeepCopy()
	volName, mountPath := restoredVolume, spec.RestoreMount

	if len(spec.RestoredVolumes) > 0 {
		volName, mountPath = spec.RestoredVolumes[0].Name, spec.RestoredVolumes[0].MountPath
	}

	volumes := []corev1.Volume{pvcVolume(volName, restoredClaim)}
	mounts := []corev1.VolumeMount{{Name: volName, MountPath: mountPath}}

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
			RestartPolicy:      corev1.RestartPolicyNever,
			InitContainers:     withMounts(spec.InitContainers, mounts),
			Containers:         withMounts(spec.Containers, mounts),
			Volumes:            volumes,
			SecurityContext:    spec.SecurityContext,
			ServiceAccountName: spec.ServiceAccountName,
			ImagePullSecrets:   spec.ImagePullSecrets,
			NodeSelector:       spec.NodeSelector,
			Tolerations:        spec.Tolerations,
			Affinity:           spec.Affinity,
			PriorityClassName:  spec.PriorityClassName,
			RuntimeClassName:   spec.RuntimeClassName,
		},
	}, nil
}

// withMounts returns copies of containers with mounts appended, leaving the check untouched.
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
