package controller

import (
	"os"
	"path/filepath"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/yaml"
)

// Every YAML the repo ships must pass the CRD schema and CEL rules, with unknown fields rejected.
var _ = Describe("shipped YAML", func() {
	for _, glob := range []string{"checks/*.yaml", "examples/*.yaml", "config/samples/*.yaml"} {
		files, err := filepath.Glob(filepath.Join("..", "..", glob))
		Expect(err).NotTo(HaveOccurred())

		for _, f := range files {
			if filepath.Base(f) == "kustomization.yaml" {
				continue
			}

			It("is accepted: "+glob+" "+filepath.Base(f), func() {
				raw, err := os.ReadFile(f)
				Expect(err).NotTo(HaveOccurred())

				obj := &unstructured.Unstructured{}
				Expect(yaml.Unmarshal(raw, &obj.Object)).To(Succeed())
				obj.SetNamespace(testNamespace)

				Expect(k8sClient.Create(ctx, obj, client.DryRunAll, client.FieldValidation("Strict"))).To(Succeed())
			})
		}
	}
})
