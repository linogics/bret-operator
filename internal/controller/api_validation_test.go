package controller

import (
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	restorev1alpha1 "github.com/linogics/bret-operator/api/v1alpha1"
)

var _ = Describe("CRD validation", func() {
	newRestore := func(name string, provider restorev1alpha1.Provider, sources ...restorev1alpha1.RestoreSource) *restorev1alpha1.Restore {
		return &restorev1alpha1.Restore{
			ObjectMeta: metav1.ObjectMeta{Name: name, Namespace: testNamespace},
			Spec: restorev1alpha1.RestoreSpec{
				Provider:        provider,
				Sources:         sources,
				RestoreCheckRef: restorev1alpha1.RestoreCheckRef{Name: "bp"},
			},
		}
	}

	src := func(pvc string) restorev1alpha1.RestoreSource {
		return restorev1alpha1.RestoreSource{
			SourcePVC: pvc,
			BackupRef: restorev1alpha1.BackupRef{Name: testBackupName},
		}
	}

	It("rejects velero with two sources", func() {
		r := newRestore("v-two", restorev1alpha1.ProviderVelero,
			restorev1alpha1.RestoreSource{BackupRef: restorev1alpha1.BackupRef{Name: "a"}},
			restorev1alpha1.RestoreSource{BackupRef: restorev1alpha1.BackupRef{Name: "b"}})

		Expect(k8sClient.Create(ctx, r)).To(MatchError(ContainSubstring("velero")))
	})

	It("rejects both selectors on one source", func() {
		s := restorev1alpha1.RestoreSource{
			BackupRef:        restorev1alpha1.BackupRef{Name: "a"},
			LabelSelector:    &metav1.LabelSelector{MatchLabels: map[string]string{"a": "b"}},
			OrLabelSelectors: []*metav1.LabelSelector{{MatchLabels: map[string]string{"c": "d"}}},
		}
		r := newRestore("v-both", restorev1alpha1.ProviderVelero, s)

		Expect(k8sClient.Create(ctx, r)).To(MatchError(ContainSubstring("mutually exclusive")))
	})

	It("rejects kopiur without sourcePVC", func() {
		s := src("")
		r := newRestore("k-nopvc", restorev1alpha1.ProviderKopiur, s)

		Expect(k8sClient.Create(ctx, r)).NotTo(Succeed())
	})

	It("rejects empty restoredVolumes", func() {
		c := &restorev1alpha1.RestoreCheck{
			ObjectMeta: metav1.ObjectMeta{Name: "empty-vols", Namespace: testNamespace},
			Spec: restorev1alpha1.RestoreCheckSpec{
				RestoredVolumes: []restorev1alpha1.RestoredVolume{},
				Containers:      []corev1.Container{{Name: "c", Image: testImage}},
			},
		}

		Expect(k8sClient.Create(ctx, c)).NotTo(Succeed())
	})

	It("accepts two kopiur sources", func() {
		r := newRestore("k-two", restorev1alpha1.ProviderKopiur, src("html"), src("pgdata"))

		Expect(k8sClient.Create(ctx, r)).To(Succeed())
		Expect(k8sClient.Delete(ctx, r)).To(Succeed())
	})
})
