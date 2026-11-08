package main

import (
	"testing"

	restorev1alpha1 "github.com/linogics/bret-operator/api/v1alpha1"
)

func TestParseProviders(t *testing.T) {
	tests := []struct {
		name string
		raw  string
		set  bool
		want map[restorev1alpha1.Provider]bool
	}{
		{"unset serves all", "", false, nil},
		{"none serves none", "none", true, map[restorev1alpha1.Provider]bool{}},
		{"empty serves none", "", true, map[restorev1alpha1.Provider]bool{}},
		{"list", "kopiur, velero", true, map[restorev1alpha1.Provider]bool{"kopiur": true, "velero": true}},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := parseProviders(tt.raw, tt.set)
			if (got == nil) != (tt.want == nil) || len(got) != len(tt.want) {
				t.Fatalf("got %v, want %v", got, tt.want)
			}

			for k := range tt.want {
				if !got[k] {
					t.Fatalf("got %v, want %v", got, tt.want)
				}
			}
		})
	}
}
