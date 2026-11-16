package controller

import (
	"io/fs"
	"os"
	"path/filepath"
	"strings"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/yaml"
)

// shippedYAML lists the YAML files under the repo dirs. examples/ is walked to any depth.
func shippedYAML() []string {
	root := filepath.Join("..", "..")

	var files []string

	for _, dir := range []string{"checks", "examples", "config/samples"} {
		err := filepath.WalkDir(filepath.Join(root, dir), func(path string, d fs.DirEntry, err error) error {
			if err != nil || d.IsDir() || filepath.Ext(path) != ".yaml" || d.Name() == "kustomization.yaml" {
				return err
			}

			files = append(files, path)

			return nil
		})
		Expect(err).NotTo(HaveOccurred())
	}

	return files
}

// Every bret object the repo ships must pass the CRD schema and CEL rules, with unknown fields rejected.
// Other kinds in the same files (Deployments, backup tool objects) are skipped.
var _ = Describe("shipped YAML", func() {
	files := shippedYAML()
	Expect(files).NotTo(BeEmpty())

	for _, f := range files {
		It("is accepted: "+strings.TrimPrefix(f, filepath.Join("..", "..")+string(filepath.Separator)), func() {
			raw, err := os.ReadFile(f)
			Expect(err).NotTo(HaveOccurred())

			for _, doc := range strings.Split(string(raw), "\n---\n") {
				obj := &unstructured.Unstructured{}
				Expect(yaml.Unmarshal([]byte(doc), &obj.Object)).To(Succeed())

				if !strings.HasPrefix(obj.GetAPIVersion(), "bret.linogics.io/") {
					continue
				}

				obj.SetNamespace(testNamespace)

				Expect(k8sClient.Create(ctx, obj, client.DryRunAll, client.FieldValidation("Strict"))).To(Succeed())
			}
		})
	}
})
