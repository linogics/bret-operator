package controller

import (
	"context"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/meta"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	restorev1alpha1 "github.com/linogics/bret-operator/api/v1alpha1"
)

var _ = Describe("RestoreCheck Controller", func() {
	Context("When reconciling a resource", func() {
		const resourceName = "test-resource"

		ctx := context.Background()

		typeNamespacedName := types.NamespacedName{
			Name:      resourceName,
			Namespace: testNamespace,
		}
		restorecheck := &restorev1alpha1.RestoreCheck{}

		BeforeEach(func() {
			By("creating the custom resource for the Kind RestoreCheck")

			err := k8sClient.Get(ctx, typeNamespacedName, restorecheck)
			if err != nil && errors.IsNotFound(err) {
				resource := &restorev1alpha1.RestoreCheck{
					ObjectMeta: metav1.ObjectMeta{
						Name:      resourceName,
						Namespace: testNamespace,
					},
					Spec: restorev1alpha1.RestoreCheckSpec{
						RestoreMount: "/restore",
						Containers:   []corev1.Container{{Name: testCheckName, Image: testImage}},
					},
				}
				Expect(k8sClient.Create(ctx, resource)).To(Succeed())
			}
		})

		AfterEach(func() {
			resource := &restorev1alpha1.RestoreCheck{}
			err := k8sClient.Get(ctx, typeNamespacedName, resource)
			Expect(err).NotTo(HaveOccurred())

			By("Cleanup the specific resource instance RestoreCheck")
			Expect(k8sClient.Delete(ctx, resource)).To(Succeed())
		})
		It("should successfully reconcile the resource", func() {
			By("Reconciling the created resource")

			controllerReconciler := &RestoreCheckReconciler{
				Client: k8sClient,
				Scheme: k8sClient.Scheme(),
			}

			_, err := controllerReconciler.Reconcile(ctx, reconcile.Request{
				NamespacedName: typeNamespacedName,
			})
			Expect(err).NotTo(HaveOccurred())

			By("Checking the Ready condition is true for a valid spec")

			got := &restorev1alpha1.RestoreCheck{}
			Expect(k8sClient.Get(ctx, typeNamespacedName, got)).To(Succeed())
			Expect(meta.IsStatusConditionTrue(got.Status.Conditions, "Ready")).To(BeTrue())
		})

		It("should mark a check with two restoredVolumes as Invalid", func() {
			got := &restorev1alpha1.RestoreCheck{}
			Expect(k8sClient.Get(ctx, typeNamespacedName, got)).To(Succeed())

			got.Spec.RestoreMount = ""
			got.Spec.RestoredVolumes = []restorev1alpha1.RestoredVolume{
				{Name: "a", MountPath: "/a", SourcePVC: "pvc-a"},
				{Name: "b", MountPath: "/b", SourcePVC: "pvc-b"},
			}
			Expect(k8sClient.Update(ctx, got)).To(Succeed())

			controllerReconciler := &RestoreCheckReconciler{Client: k8sClient, Scheme: k8sClient.Scheme()}

			_, err := controllerReconciler.Reconcile(ctx, reconcile.Request{NamespacedName: typeNamespacedName})
			Expect(err).NotTo(HaveOccurred())

			Expect(k8sClient.Get(ctx, typeNamespacedName, got)).To(Succeed())

			ready := meta.FindStatusCondition(got.Status.Conditions, "Ready")
			Expect(ready).NotTo(BeNil())
			Expect(ready.Status).To(Equal(metav1.ConditionFalse))
			Expect(ready.Reason).To(Equal("Invalid"))
		})
	})
})
