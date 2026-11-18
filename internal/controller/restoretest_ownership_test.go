package controller

import (
	"fmt"
	"testing"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	restorev1alpha1 "github.com/linogics/bret-operator/api/v1alpha1"
)

// A Restore owned by another (deleted) RestoreTest must not count as this test's run.
func TestIgnoresRestoreOfOtherTest(t *testing.T) {
	test := newRestoreTestObject()
	test.UID = "new-uid"
	test.Annotations = map[string]string{runNowAnnotation: "v"}
	test.Spec.Triggers = &restorev1alpha1.Triggers{Manual: true}

	run := runID("run-now-v")
	isController := true
	old := &restorev1alpha1.Restore{
		ObjectMeta: metav1.ObjectMeta{
			Name:      fmt.Sprintf("%s-%s-a1", test.Name, run),
			Namespace: test.Namespace,
			Labels:    map[string]string{testLabel: test.Name, runLabel: run, attemptLabel: "1"},
			OwnerReferences: []metav1.OwnerReference{{
				APIVersion: restorev1alpha1.GroupVersion.String(), Kind: "RestoreTest",
				Name: test.Name, UID: "old-uid", Controller: &isController,
			}},
		},
		Spec:   restorev1alpha1.RestoreSpec{Provider: restorev1alpha1.ProviderKopiur, RestoreCheckRef: test.Spec.RestoreCheckRef},
		Status: restorev1alpha1.RestoreStatus{Phase: restorev1alpha1.RestorePhaseSucceeded, Message: "old run"},
	}

	f := newRestoreTestFixture(t, test, old)
	f.run()

	if ls := f.test().Status.LastSuccess; ls != nil {
		t.Fatalf("LastSuccess set to %v from a Restore owned by another RestoreTest", ls)
	}
}
