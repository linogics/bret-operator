package controller

import (
	"os"
	"slices"
	"testing"

	rbacv1 "k8s.io/api/rbac/v1"
	"sigs.k8s.io/yaml"
)

const veleroRestoreResource = "restores"

// KeepOnFailure patches the retained label onto Velero Restores and ReplicationDestinations,
// so the role must grant patch on them.
func TestRoleCanLabelRetained(t *testing.T) {
	raw, err := os.ReadFile("../../config/rbac/role.yaml")
	if err != nil {
		t.Fatal(err)
	}

	role := rbacv1.ClusterRole{}
	if err := yaml.Unmarshal(raw, &role); err != nil {
		t.Fatal(err)
	}

	for _, want := range [][2]string{{veleroGroup, veleroRestoreResource}, {"volsync.backube", "replicationdestinations"}} {
		ok := false

		for _, rule := range role.Rules {
			if slices.Contains(rule.APIGroups, want[0]) && slices.Contains(rule.Resources, want[1]) && slices.Contains(rule.Verbs, "patch") {
				ok = true
			}
		}

		if !ok {
			t.Errorf("role grants no patch on %s %s", want[0], want[1])
		}
	}
}
