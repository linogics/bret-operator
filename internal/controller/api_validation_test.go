package controller

import (
	"fmt"

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

	It("rejects a velero source without sourcePVC", func() {
		r := newRestore("v-nopvc", restorev1alpha1.ProviderVelero, src(""))

		Expect(k8sClient.Create(ctx, r)).To(MatchError(ContainSubstring("sourcePVC")))
	})

	It("rejects velero sources with different backupRefs", func() {
		other := src("mysql")
		other.BackupRef.Name = "other"
		r := newRestore("v-diff", restorev1alpha1.ProviderVelero, src("wp-content"), other)

		Expect(k8sClient.Create(ctx, r)).To(MatchError(ContainSubstring("same backupRef")))
	})

	It("accepts two velero sources with the same backupRef", func() {
		r := newRestore("v-two", restorev1alpha1.ProviderVelero, src("wp-content"), src("mysql"))

		Expect(k8sClient.Create(ctx, r)).To(Succeed())
		Expect(k8sClient.Delete(ctx, r)).To(Succeed())
	})

	DescribeTable("rejects spec.velero with another provider", func(p restorev1alpha1.Provider) {
		r := newRestore("o-velero", p, src("html"))
		r.Spec.Velero = &restorev1alpha1.VeleroOptions{LabelSelector: &metav1.LabelSelector{MatchLabels: map[string]string{"a": "b"}}}

		Expect(k8sClient.Create(ctx, r)).To(MatchError(ContainSubstring("velero is only allowed")))
	},
		Entry("kopiur", restorev1alpha1.ProviderKopiur),
		Entry("volsync", restorev1alpha1.ProviderVolSync),
	)

	DescribeTable("rejects restoreAsOf with another provider", func(p restorev1alpha1.Provider) {
		s := src("html")
		s.RestoreAsOf = &metav1.Time{Time: metav1.Now().Time}
		r := newRestore("o-asof", p, s)

		Expect(k8sClient.Create(ctx, r)).To(MatchError(ContainSubstring("restoreAsOf")))
	},
		Entry("kopiur", restorev1alpha1.ProviderKopiur),
		Entry("velero", restorev1alpha1.ProviderVelero),
	)

	It("rejects spec.velero with both selectors", func() {
		r := newRestore("v-both", restorev1alpha1.ProviderVelero, src("html"))
		r.Spec.Velero = &restorev1alpha1.VeleroOptions{
			LabelSelector:    &metav1.LabelSelector{MatchLabels: map[string]string{"a": "b"}},
			OrLabelSelectors: []*metav1.LabelSelector{{MatchLabels: map[string]string{"c": "d"}}},
		}

		Expect(k8sClient.Create(ctx, r)).To(MatchError(ContainSubstring("mutually exclusive")))
	})

	It("rejects kopiur without sourcePVC", func() {
		r := newRestore("k-nopvc", restorev1alpha1.ProviderKopiur, src(""))

		Expect(k8sClient.Create(ctx, r)).To(MatchError(ContainSubstring("sourcePVC")))
	})

	It("rejects duplicate sourcePVC", func() {
		r := newRestore("k-dup", restorev1alpha1.ProviderKopiur, src("html"), src("html"))

		Expect(k8sClient.Create(ctx, r)).To(MatchError(ContainSubstring("sourcePVC must be unique")))
	})

	It("rejects a velero source with size", func() {
		s := src("html")
		s.Size = quantityPtr("1Gi")
		r := newRestore("v-size", restorev1alpha1.ProviderVelero, s)

		Expect(k8sClient.Create(ctx, r)).To(MatchError(ContainSubstring("same backupRef")))
	})

	DescribeTable("rejects a bad sourcePVC", func(pvc string) {
		r := newRestore("bad-pvc", restorev1alpha1.ProviderKopiur, src(pvc))

		Expect(k8sClient.Create(ctx, r)).To(MatchError(ContainSubstring("sourcePVC")))
	},
		Entry("uppercase", "Data"),
		Entry("dot", "data.db"),
	)

	It("rejects more than 10 sources", func() {
		srcs := make([]restorev1alpha1.RestoreSource, 0, 11)
		for i := range 11 {
			srcs = append(srcs, src(fmt.Sprintf("vol%d", i)))
		}

		r := newRestore("k-many", restorev1alpha1.ProviderKopiur, srcs...)

		Expect(k8sClient.Create(ctx, r)).To(MatchError(ContainSubstring("spec.sources")))
	})

	newTest := func(name string, sources ...restorev1alpha1.RestoreTestSource) *restorev1alpha1.RestoreTest {
		return &restorev1alpha1.RestoreTest{
			ObjectMeta: metav1.ObjectMeta{Name: name, Namespace: testNamespace},
			Spec: restorev1alpha1.RestoreTestSpec{
				Provider:        restorev1alpha1.ProviderKopiur,
				Sources:         sources,
				RestoreCheckRef: restorev1alpha1.RestoreCheckRef{Name: "bp"},
			},
		}
	}

	rtSrc := func(pvc string) restorev1alpha1.RestoreTestSource {
		return restorev1alpha1.RestoreTestSource{
			SourcePVC:      pvc,
			BackupSelector: restorev1alpha1.BackupSelector{Name: "b"},
		}
	}

	It("rejects a RestoreTest without sources", func() {
		Expect(k8sClient.Create(ctx, newTest("rt-none"))).To(MatchError(ContainSubstring("spec.sources")))
	})

	It("rejects a RestoreTest with duplicate sourcePVC", func() {
		Expect(k8sClient.Create(ctx, newTest("rt-dup", rtSrc("html"), rtSrc("html")))).To(MatchError(ContainSubstring("Duplicate value")))
	})

	It("rejects a velero RestoreTest with different backupSelectors", func() {
		other := rtSrc("mysql")
		other.BackupSelector.Name = "other"
		t := newTest("rt-vdiff", rtSrc("wp-content"), other)
		t.Spec.Provider = restorev1alpha1.ProviderVelero

		Expect(k8sClient.Create(ctx, t)).To(MatchError(ContainSubstring("same backupSelector")))
	})

	It("rejects a RestoreTest with spec.velero and provider volsync", func() {
		t := newTest("rt-vvol", rtSrc("html"))
		t.Spec.Provider = restorev1alpha1.ProviderVolSync
		t.Spec.Velero = &restorev1alpha1.VeleroOptions{LabelSelector: &metav1.LabelSelector{MatchLabels: map[string]string{"a": "b"}}}

		Expect(k8sClient.Create(ctx, t)).To(MatchError(ContainSubstring("velero is only allowed")))
	})

	It("rejects empty restoredVolumes", func() {
		c := &restorev1alpha1.RestoreCheck{
			ObjectMeta: metav1.ObjectMeta{Name: "empty-vols", Namespace: testNamespace},
			Spec: restorev1alpha1.RestoreCheckSpec{
				RestoredVolumes: []restorev1alpha1.RestoredVolume{},
				Containers:      []corev1.Container{{Name: "c", Image: testImage}},
			},
		}

		Expect(k8sClient.Create(ctx, c)).To(MatchError(ContainSubstring("restoredVolumes")))
	})

	It("accepts two kopiur sources", func() {
		r := newRestore("k-two", restorev1alpha1.ProviderKopiur, src("html"), src("pgdata"))

		Expect(k8sClient.Create(ctx, r)).To(Succeed())
		Expect(k8sClient.Delete(ctx, r)).To(Succeed())
	})
})
