// Command render prints the PVCs and the check pod of one RestoreCheck.
// It calls controller.RenderCheck, so the harness runs the same pod as the operator.
// The check harness reaches it through hack/check-test/render.py.
//
//	render CHECK.yaml --name POD [--ns NS] [--claims KEY=PVC ...]  print YAML
//	render CHECK.yaml --list                                       print volume keys, one per line
//	render CHECK.yaml --uid KEY                                    print the mover UID, or nothing
//
// KEY of --uid is ignored. The annotation key is always "data".
package main

import (
	"fmt"
	"os"
	"strings"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"sigs.k8s.io/yaml"

	restorev1alpha1 "github.com/linogics/bret-operator/api/v1alpha1"
	"github.com/linogics/bret-operator/internal/controller"
)

const usage = "usage: render CHECK.yaml [--name POD] [--ns NS] [--claims KEY=PVC ...] [--list] [--uid KEY]\n" +
	"KEY of --uid is ignored, the annotation key is always data"

type options struct {
	check  string
	name   string
	ns     string
	claims map[string]string
	list   bool
	hasUID bool
}

func main() {
	err := run(os.Args[1:])
	if err != nil {
		fmt.Fprintln(os.Stderr, "render:", err)
		os.Exit(1)
	}
}

func run(args []string) error {
	o, err := parse(args)
	if err != nil {
		return err
	}

	raw, err := os.ReadFile(o.check)
	if err != nil {
		return err
	}

	bp := &restorev1alpha1.RestoreCheck{}

	err = yaml.Unmarshal(raw, bp)
	if err != nil {
		return fmt.Errorf("parse %s: %w", o.check, err)
	}

	keys := volumeKeys(bp)

	switch {
	case o.list:
		for _, k := range keys {
			fmt.Println(k)
		}

		return nil
	case o.hasUID:
		uid, err := controller.MoverUID(bp)
		if err != nil {
			return err
		}

		if uid != nil {
			fmt.Println(*uid)
		}

		return nil
	}

	if o.name == "" {
		return fmt.Errorf("--name is required")
	}

	if len(keys) == 0 {
		return fmt.Errorf("check %s has no restored volume", bp.Name)
	}

	claim, ok := o.claims[keys[0]]
	if !ok {
		return fmt.Errorf("missing --claims %s=<pvc>; check needs keys %v", keys[0], keys)
	}

	restore := &restorev1alpha1.Restore{ObjectMeta: metav1.ObjectMeta{Name: o.name, Namespace: o.ns}}

	objs, err := controller.RenderCheck(restore, bp, claim)
	if err != nil {
		return err
	}

	for _, obj := range objs {
		kind := "PersistentVolumeClaim"

		if pod, isPod := obj.(*corev1.Pod); isPod {
			kind = "Pod"
			pod.Name = o.name // the harness names the pod itself
		}

		obj.GetObjectKind().SetGroupVersionKind(corev1.SchemeGroupVersion.WithKind(kind))

		out, err := yaml.Marshal(obj)
		if err != nil {
			return err
		}

		fmt.Printf("---\n%s", out)
	}

	return nil
}

// volumeKeys lists the keys the harness takes claims for. They are the restoredVolumes sourcePVC names.
func volumeKeys(bp *restorev1alpha1.RestoreCheck) []string {
	keys := make([]string, 0, len(bp.Spec.RestoredVolumes))
	for _, v := range bp.Spec.RestoredVolumes {
		keys = append(keys, v.SourcePVC)
	}

	return keys
}

func parse(args []string) (options, error) {
	o := options{ns: "default", claims: map[string]string{}}

	if len(args) == 0 {
		return o, fmt.Errorf("%s", usage)
	}

	o.check = args[0]

	for i := 1; i < len(args); i++ {
		switch args[i] {
		case "--list":
			o.list = true
		case "--name", "--ns", "--uid":
			if i+1 >= len(args) {
				return o, fmt.Errorf("%s needs a value", args[i])
			}

			i++

			switch args[i-1] {
			case "--name":
				o.name = args[i]
			case "--ns":
				o.ns = args[i]
			default:
				o.hasUID = true
			}
		case "--claims":
			for i+1 < len(args) && !strings.HasPrefix(args[i+1], "--") {
				i++

				k, v, ok := strings.Cut(args[i], "=")
				if !ok {
					return o, fmt.Errorf("claim %q must be KEY=PVC", args[i])
				}

				o.claims[k] = v
			}
		default:
			return o, fmt.Errorf("unknown argument %q. %s", args[i], usage)
		}
	}

	return o, nil
}
