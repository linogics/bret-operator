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

		Expect(k8sClient.Create(ctx, r)).To(MatchError(ContainSubstring("sources need a sourcePVC")))
	})

	It("rejects duplicate sourcePVC", func() {
		r := newRestore("k-dup", restorev1alpha1.ProviderKopiur, src("html"), src("html"))

		Expect(k8sClient.Create(ctx, r)).To(MatchError(ContainSubstring("sourcePVC must be unique")))
	})

	It("rejects a kopiur source with labelSelector", func() {
		s := src("html")
		s.LabelSelector = &metav1.LabelSelector{MatchLabels: map[string]string{"a": "b"}}
		r := newRestore("k-sel", restorev1alpha1.ProviderKopiur, s)

		Expect(k8sClient.Create(ctx, r)).To(MatchError(ContainSubstring("no label selectors")))
	})

	It("rejects a velero source with sourcePVC", func() {
		r := newRestore("v-pvc", restorev1alpha1.ProviderVelero, src("html"))

		Expect(k8sClient.Create(ctx, r)).To(MatchError(ContainSubstring("without sourcePVC")))
	})

	It("rejects a velero source with size", func() {
		s := restorev1alpha1.RestoreSource{BackupRef: restorev1alpha1.BackupRef{Name: "a"}}
		s.Size = quantityPtr("1Gi")
		r := newRestore("v-size", restorev1alpha1.ProviderVelero, s)

		Expect(k8sClient.Create(ctx, r)).To(MatchError(ContainSubstring("without sourcePVC, size")))
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
