package controller

import (
	"errors"

	"k8s.io/apimachinery/pkg/api/resource"

	restorev1alpha1 "github.com/linogics/bret-operator/api/v1alpha1"
)

var (
	// ErrSizeUnknown means the source size is unknown and target.size is not set.
	ErrSizeUnknown = errors.New(restorev1alpha1.ReasonSizeUnknown + ": target.size is required when the source size is unknown")
	// ErrInsufficientCapacity means target.size is smaller than the source size.
	ErrInsufficientCapacity = errors.New(restorev1alpha1.ReasonInsufficientCapacity + ": target.size is smaller than the source size")
)

// resolveSize returns the scratch size of a Restore. source is nil when the source size is unknown.
func resolveSize(target restorev1alpha1.RestoreTarget, source *resource.Quantity) (resource.Quantity, error) {
	if source == nil {
		if target.Size == nil {
			return resource.Quantity{}, ErrSizeUnknown
		}

		return *target.Size, nil
	}

	if target.Size != nil {
		if target.Size.Cmp(*source) < 0 {
			return resource.Quantity{}, ErrInsufficientCapacity
		}

		return *target.Size, nil
	}

	expand := target.AutoExpand
	if expand == nil {
		minSize := resource.MustParse("1Gi")
		expand = &restorev1alpha1.AutoExpand{Percentage: 10, Min: &minSize}
	}

	size := resource.NewQuantity(source.Value()*int64(expand.Percentage)/100, resource.BinarySI)

	if expand.Min != nil && size.Cmp(*expand.Min) < 0 {
		return *expand.Min, nil
	}

	if expand.Max != nil && size.Cmp(*expand.Max) > 0 {
		return *expand.Max, nil
	}

	return *size, nil
}
