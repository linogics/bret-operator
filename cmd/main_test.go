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
		err  bool
	}{
		{"unset serves all", "", false, nil, false},
		{"none serves none", "none", true, map[restorev1alpha1.Provider]bool{}, false},
		{"empty serves none", "", true, map[restorev1alpha1.Provider]bool{}, false},
		{"list", "kopiur, velero", true, map[restorev1alpha1.Provider]bool{"kopiur": true, "velero": true}, false},
		{"unknown is an error", "kopia", true, nil, true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := parseProviders(tt.raw, tt.set)
			if (err != nil) != tt.err {
				t.Fatalf("err = %v, want error: %v", err, tt.err)
			}

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
