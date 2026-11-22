package controller

import (
	"testing"

	"sigs.k8s.io/controller-runtime/pkg/client"

	restorev1alpha1 "github.com/linogics/bret-operator/api/v1alpha1"
)

func TestProviderEnabled(t *testing.T) {
	all := &RestoreReconciler{}
	if !all.providerEnabled(restorev1alpha1.ProviderVelero) {
		t.Fatal("nil EnabledProviders must serve every provider")
	}

	if all.providerEnabled("other") {
		t.Fatal("unknown provider must be rejected")
	}

	some := &RestoreReconciler{EnabledProviders: map[restorev1alpha1.Provider]bool{restorev1alpha1.ProviderKopiur: true}}
	if !some.providerEnabled(restorev1alpha1.ProviderKopiur) || some.providerEnabled(restorev1alpha1.ProviderVolSync) {
		t.Fatal("only listed providers must be served")
	}
}

func TestRestoreDisabledProviderFails(t *testing.T) {
	restore := newTestRestore()
	c, r := fakeRestoreClient(t, restore, newTestCheck())
	r.EnabledProviders = map[restorev1alpha1.Provider]bool{restorev1alpha1.ProviderVelero: true}
	key := client.ObjectKeyFromObject(restore)

	reconcileRestore(t, r, key)

	if got := getRestore(t, c, key); got.Status.Phase != restorev1alpha1.RestorePhaseFailed || readyReason(got) != reasonProviderUnsupported {
		t.Fatalf("phase %q reason %q, want Failed with %s", got.Status.Phase, readyReason(got), reasonProviderUnsupported)
	}
}
