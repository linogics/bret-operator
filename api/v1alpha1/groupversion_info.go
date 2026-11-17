// Package v1alpha1 contains API Schema definitions for the restore v1alpha1 API group.
// +kubebuilder:object:generate=true
// +groupName=bret.linogics.io
package v1alpha1

import (
	"k8s.io/apimachinery/pkg/runtime/schema"
	"sigs.k8s.io/controller-runtime/pkg/scheme"
)

var (
	// GroupVersion is the group version that registers these objects.
	GroupVersion = schema.GroupVersion{Group: "bret.linogics.io", Version: "v1alpha1"}

	// SchemeBuilder adds Go types to the GroupVersionKind scheme.
	SchemeBuilder = &scheme.Builder{GroupVersion: GroupVersion}

	// AddToScheme adds the types of this group version to the given scheme.
	AddToScheme = SchemeBuilder.AddToScheme
)
