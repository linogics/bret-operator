package controller

import (
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	restorev1alpha1 "github.com/linogics/bret-operator/api/v1alpha1"
)

// A Restore spec cannot change after creation, so its status always describes the backups in the spec.
var _ = Describe("Restore spec validation", func() {
	It("rejects a spec change after creation", func() {
		r := &restorev1alpha1.Restore{
			ObjectMeta: metav1.ObjectMeta{Name: "immutable-spec", Namespace: testNamespace},
			Spec: restorev1alpha1.RestoreSpec{
				Provider:        restorev1alpha1.ProviderKopiur,
				Sources:         []restorev1alpha1.RestoreSource{{SourcePVC: "data", BackupRef: restorev1alpha1.BackupRef{Name: "b1"}}},
				RestoreCheckRef: restorev1alpha1.RestoreCheckRef{Name: "bp"},
			},
		}
		Expect(k8sClient.Create(ctx, r)).To(Succeed())

		DeferCleanup(func() { Expect(k8sClient.Delete(ctx, r)).To(Succeed()) })

		r.Spec.Sources[0].BackupRef.Name = "b2"
		Expect(k8sClient.Update(ctx, r)).To(MatchError(ContainSubstring("spec is immutable")))
	})
})
