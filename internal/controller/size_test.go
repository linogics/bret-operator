package controller

import (
	"testing"

	"k8s.io/apimachinery/pkg/api/resource"

	restorev1alpha1 "github.com/linogics/bret-operator/api/v1alpha1"
)

func TestResolveSize(t *testing.T) {
	tests := []struct {
		name    string
		target  restorev1alpha1.RestoreTarget
		source  *resource.Quantity
		want    string
		wantErr error
	}{
		{
			name:   "target size without source",
			target: restorev1alpha1.RestoreTarget{Size: quantityPtr(testSize1Gi)},
			want:   testSize1Gi,
		},
		{
			name:    "no size and no source",
			target:  restorev1alpha1.RestoreTarget{},
			wantErr: ErrSizeUnknown,
		},
		{
			name:    "target size smaller than source",
			target:  restorev1alpha1.RestoreTarget{Size: quantityPtr(testSize1Gi)},
			source:  quantityPtr("2Gi"),
			wantErr: ErrInsufficientCapacity,
		},
		{
			name:   "default applies min",
			target: restorev1alpha1.RestoreTarget{},
			source: quantityPtr("10Mi"),
			want:   testSize1Gi,
		},
		{
			name:   "headroom on top of the source",
			target: restorev1alpha1.RestoreTarget{AutoExpand: &restorev1alpha1.AutoExpand{Percentage: 10, Min: quantityPtr(testSize1Gi)}},
			source: quantityPtr(testSize10Gi),
			want:   "11Gi",
		},
		{
			name:   "default on a 20Gi source is 22Gi",
			target: restorev1alpha1.RestoreTarget{},
			source: quantityPtr("20Gi"),
			want:   "22Gi",
		},
		{
			name:    "max below the source is insufficient",
			target:  restorev1alpha1.RestoreTarget{AutoExpand: &restorev1alpha1.AutoExpand{Percentage: 10, Max: quantityPtr(testSize5Gi)}},
			source:  quantityPtr("100Gi"),
			wantErr: ErrInsufficientCapacity,
		},
		{
			name:   "max clamps",
			target: restorev1alpha1.RestoreTarget{AutoExpand: &restorev1alpha1.AutoExpand{Percentage: 100, Max: quantityPtr("7Gi")}},
			source: quantityPtr(testSize5Gi),
			want:   "7Gi",
		},
		{
			name: "fixed size idiom",
			target: restorev1alpha1.RestoreTarget{AutoExpand: &restorev1alpha1.AutoExpand{
				Percentage: 100, Min: quantityPtr(testSize5Gi), Max: quantityPtr(testSize5Gi),
			}},
			source: quantityPtr("3Gi"),
			want:   testSize5Gi,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := resolveSize(tt.target, tt.source)
			if err != tt.wantErr {
				t.Fatalf("err = %v, want %v", err, tt.wantErr)
			}

			if tt.wantErr != nil {
				return
			}

			want := resource.MustParse(tt.want)
			if got.Cmp(want) != 0 {
				t.Errorf("size = %s, want %s", got.String(), tt.want)
			}
		})
	}
}
