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
			target: restorev1alpha1.RestoreTarget{Size: quantityPtr("1Gi")},
			want:   "1Gi",
		},
		{
			name:    "no size and no source",
			target:  restorev1alpha1.RestoreTarget{},
			wantErr: ErrSizeUnknown,
		},
		{
			name:    "target size smaller than source",
			target:  restorev1alpha1.RestoreTarget{Size: quantityPtr("1Gi")},
			source:  quantityPtr("2Gi"),
			wantErr: ErrInsufficientCapacity,
		},
		{
			name:   "default applies min",
			target: restorev1alpha1.RestoreTarget{},
			source: quantityPtr("10Mi"),
			want:   "1Gi",
		},
		{
			name:   "exact percentage",
			target: restorev1alpha1.RestoreTarget{AutoExpand: &restorev1alpha1.AutoExpand{Percentage: 10, Min: quantityPtr("1Gi")}},
			source: quantityPtr("10Gi"),
			want:   "1Gi",
		},
		{
			name:   "max clamps",
			target: restorev1alpha1.RestoreTarget{AutoExpand: &restorev1alpha1.AutoExpand{Percentage: 10, Max: quantityPtr("5Gi")}},
			source: quantityPtr("100Gi"),
			want:   "5Gi",
		},
		{
			name: "fixed size idiom",
			target: restorev1alpha1.RestoreTarget{AutoExpand: &restorev1alpha1.AutoExpand{
				Percentage: 100, Min: quantityPtr("5Gi"), Max: quantityPtr("5Gi"),
			}},
			source: quantityPtr("3Gi"),
			want:   "5Gi",
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
