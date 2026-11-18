package controller

import (
	"time"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	restorev1alpha1 "github.com/linogics/bret-operator/api/v1alpha1"
)

// A negative restoreWindow must be rejected by the CRD.
var _ = Describe("Restore window validation", func() {
	It("rejects a negative restoreWindow", func() {
		r := &restorev1alpha1.Restore{
			ObjectMeta: metav1.ObjectMeta{Name: "negative-window", Namespace: testNamespace},
			Spec: restorev1alpha1.RestoreSpec{
				Provider: restorev1alpha1.ProviderKopiur,
				Sources: []restorev1alpha1.RestoreSource{{
					SourcePVC: "data",
					BackupRef: restorev1alpha1.BackupRef{Name: testBackupName},
				}},
				RestoreCheckRef: restorev1alpha1.RestoreCheckRef{Name: "bp"},
				RestoreWindow:   metav1.Duration{Duration: -5 * time.Minute},
			},
		}

		Expect(k8sClient.Create(ctx, r)).To(MatchError(ContainSubstring("restoreWindow")))
	})
})
