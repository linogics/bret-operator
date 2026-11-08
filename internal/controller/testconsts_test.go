package controller

import "k8s.io/apimachinery/pkg/api/resource"

const (
	testNamespace  = "default"
	testApp        = "postgres"
	testMountPath  = "/var/lib/postgresql/data"
	testCheckName  = "check"
	testLabelKey   = "app"
	testLabelValue = "pg"

	testDataName      = "data"
	testBackupName    = "backup"
	testStatusKey     = "status"
	testWorkArea      = "Work Area"
	testWorkAreaClaim = "r1-scratch-work-area"
	testSize1Gi       = "1Gi"
	testSize5Gi       = "5Gi"
	testSize10Gi      = "10Gi"
	testPVCKeyA       = "ns/a"
	testPVCKeyB       = "ns/b"
	testRepoSecret    = "repo-secret"
)

func quantityPtr(s string) *resource.Quantity {
	q := resource.MustParse(s)

	return &q
}
