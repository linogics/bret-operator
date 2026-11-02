package controller

import "k8s.io/apimachinery/pkg/api/resource"

const (
	testNamespace  = "default"
	testApp        = "postgres"
	testMountPath  = "/var/lib/postgresql/data"
	testCheckName  = "check"
	testLabelKey   = "app"
	testLabelValue = "pg"
)

func quantityPtr(s string) *resource.Quantity {
	q := resource.MustParse(s)

	return &q
}
