package controller

import (
	"crypto/sha256"
	"encoding/hex"
	"strings"

	restorev1alpha1 "github.com/linogics/bret-operator/api/v1alpha1"
)

const (
	maxNameLen    = 63
	maxSuffixLen  = 40
	nameHashChars = 6
)

// boundedName is base+suffix, kept to a valid DNS label of at most 63 characters.
// A long name is cut and gets a short hash, so different inputs stay different. The suffix is kept when short.
func boundedName(base, suffix string) string {
	full := base + suffix
	if len(full) <= maxNameLen {
		return full
	}

	sum := sha256.Sum256([]byte(full))
	hash := hex.EncodeToString(sum[:])[:nameHashChars]

	if len(suffix) > maxSuffixLen {
		base, suffix = full, ""
	}

	cut := strings.TrimRight(base[:maxNameLen-1-nameHashChars-len(suffix)], "-.")

	return cut + "-" + hash + suffix
}

// restoredClaimName is the PVC that receives the restored data of one source.
func restoredClaimName(restore *restorev1alpha1.Restore, sourcePVC string) string {
	return boundedName(restore.Name, "-"+sourcePVC)
}

// scratchClaimName is the PVC of one check scratch volume.
func scratchClaimName(restore *restorev1alpha1.Restore, volume string) string {
	return boundedName(restore.Name, "-scratch-"+dnsLabel(volume))
}

// kopiurName is the name of the Kopiur Restore of one source.
func kopiurName(restore *restorev1alpha1.Restore, sourcePVC string) string {
	return boundedName(restore.Name, "-kopiur-"+sourcePVC)
}

// volsyncName is the name of the VolSync ReplicationDestination of one source.
func volsyncName(restore *restorev1alpha1.Restore, sourcePVC string) string {
	return boundedName(restore.Name, "-volsync-"+sourcePVC)
}

// restoredVolumeName is the pod volume name of one restored volume.
func restoredVolumeName(sourcePVC string) string {
	return boundedName("restored-"+sourcePVC, "")
}
