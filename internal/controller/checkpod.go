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
	restoreLabel   = "restore.bret.linogics.io/restore"
	blueprintLabel = "restore.bret.linogics.io/blueprint"
	restoredVolume = "restored-data"
)

var nonDNSLabelChars = regexp.MustCompile(`[^a-z0-9-]`)

// dnsLabel mirrors dns() in hack/blueprint-test/render.py, so names match the rendered test pods.
func dnsLabel(s string) string {
	s = nonDNSLabelChars.ReplaceAllString(strings.ToLower(s), "-")
	s = strings.Trim(s, "-")

	if len(s) > 50 {
		s = s[:50]
	}

	return s
}

// validateBlueprint rejects blueprints that this round cannot run.
func validateBlueprint(bp *restorev1alpha1.RestoreBlueprint) error {
	if len(bp.Spec.RestoredVolumes) > 0 {
		return fmt.Errorf("blueprint %s: restoredVolumes is not supported yet", bp.Name)
	}

	if bp.Spec.RestoreMount == "" {
		return fmt.Errorf("blueprint %s: restoreMount is required", bp.Name)
	}

	return nil
}

// checkPodName is the name of the check pod of one restore attempt.
func checkPodName(restore *restorev1alpha1.Restore) string {
	return restore.Name + "-check"
}

// buildCheckPod renders the check pod for one attempt, the same way hack/blueprint-test/render.py does.
// restoredClaim is mounted at restoreMount; scratchClaims maps scratch volume names to their PVC names.
func buildCheckPod(
	restore *restorev1alpha1.Restore,
	bp *restorev1alpha1.RestoreBlueprint,
	restoredClaim string,
	scratchClaims map[string]string,
) (*corev1.Pod, error) {
	err := validateBlueprint(bp)
	if err != nil {
		return nil, err
	}

	spec := bp.Spec.DeepCopy()
	volumes := []corev1.Volume{pvcVolume(restoredVolume, restoredClaim)}
	mounts := []corev1.VolumeMount{{Name: restoredVolume, MountPath: spec.RestoreMount}}

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
				restoreLabel:   restore.Name,
				blueprintLabel: dnsLabel(bp.Name),
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

// withMounts returns copies of containers with mounts appended, leaving the blueprint untouched.
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
