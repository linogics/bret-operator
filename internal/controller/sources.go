package controller

import (
	"context"
	"errors"
	"fmt"
	"slices"
	"strings"

	"k8s.io/apimachinery/pkg/api/resource"

	restorev1alpha1 "github.com/linogics/bret-operator/api/v1alpha1"
)

// Condition reasons for a Restore whose sources do not fit its check or its backups.
const (
	reasonSourceMissing  = "SourceMissing"
	reasonSourceMismatch = "SourceMismatch"
)

// matchSources checks that the sources of the Restore and the restored volumes of the check name the same PVCs.
// Velero restores a whole namespace and has no sourcePVC, so it always matches.
func matchSources(restore *restorev1alpha1.Restore, bp *restorev1alpha1.RestoreCheck) error {
	if restore.Spec.Provider == restorev1alpha1.ProviderVelero {
		return nil
	}

	want := make([]string, 0, len(bp.Spec.RestoredVolumes))
	for _, rv := range bp.Spec.RestoredVolumes {
		want = append(want, rv.SourcePVC)
	}

	have := make([]string, 0, len(restore.Spec.Sources))
	for _, src := range restore.Spec.Sources {
		have = append(have, src.SourcePVC)
	}

	missing := without(want, have)
	extra := without(have, want)

	if len(missing) == 0 && len(extra) == 0 {
		return nil
	}

	var parts []string
	if len(missing) > 0 {
		parts = append(parts, fmt.Sprintf("check %s restores %s, but the Restore has no such source", bp.Name, strings.Join(missing, ", ")))
	}

	if len(extra) > 0 {
		parts = append(parts, fmt.Sprintf("Restore sources %s are not restored volumes of check %s", strings.Join(extra, ", "), bp.Name))
	}

	return errors.New(strings.Join(parts, "; "))
}

// without returns the entries of a that b does not contain.
func without(a, b []string) []string {
	var out []string

	for _, s := range a {
		if !slices.Contains(b, s) {
			out = append(out, s)
		}
	}

	return out
}

// sourceSizes resolves the scratch size of every source once and stores it in the status, by sourcePVC.
// Later reconciles reuse it, so the sources are read once. Velero needs no size.
func (r *RestoreReconciler) sourceSizes(ctx context.Context, restore *restorev1alpha1.Restore) (map[string]resource.Quantity, error) {
	sizes := make(map[string]resource.Quantity, len(restore.Spec.Sources))

	if restore.Spec.Provider == restorev1alpha1.ProviderVelero {
		return sizes, nil
	}

	for _, src := range restore.Spec.Sources {
		if size, ok := restore.Status.ResolvedSizes[src.SourcePVC]; ok {
			sizes[src.SourcePVC] = size

			continue
		}

		// A Restore from before the size was stored has no size to create objects with.
		if len(restore.Status.ProviderRefs) > 0 {
			return nil, ErrSizeUnknown
		}

		source, err := r.sourceSize(ctx, restore, src)
		if err != nil {
			return nil, err
		}

		size, err := resolveSize(src.RestoreTarget, source)
		if err != nil {
			return nil, fmt.Errorf("source %s: %w", src.SourcePVC, err)
		}

		sizes[src.SourcePVC] = size
	}

	restore.Status.ResolvedSizes = sizes

	return sizes, nil
}
