/*
Copyright 2020 The Crossplane Authors.

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

    http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.
*/

package resource

import (
	"encoding/json"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"

	"github.com/crossplane/crossplane-runtime/apis/core/v1alpha1"
	"github.com/crossplane/crossplane-runtime/pkg/fieldpath"
	"github.com/crossplane/crossplane-runtime/pkg/resource"
	"github.com/crossplane/crossplane-runtime/pkg/resource/unstructured/requirement"
)

// SanitizedDeepCopyObject removes the metadata that can be specific to a cluster.
// For example, owner references are references to resources in that cluster and
// would be meaningless in another one.
func SanitizedDeepCopyObject(in runtime.Object) resource.Object {
	out, _ := in.DeepCopyObject().(resource.Object)
	out.SetResourceVersion("")
	out.SetUID("")
	out.SetCreationTimestamp(metav1.Unix(0, 0))
	out.SetSelfLink("")
	out.SetOwnerReferences(nil)
	out.SetManagedFields(nil)
	out.SetFinalizers(nil)
	return out
}

// OverrideInputMetadata copies the user-filled metadata from "from" object to "to".
func OverrideInputMetadata(from, to metav1.Object) {
	to.SetName(from.GetName())
	to.SetNamespace(from.GetNamespace())
	to.SetAnnotations(from.GetAnnotations())
	to.SetLabels(from.GetLabels())
}

// TODO(muvaf): EqualizeRequirementSpec could be separated into Propagate and
// LateInitialize functions.

// EqualizeRequirementSpec propagates desired state from "from" to "to" and updates
// the "from" object with the latest observation of the fields that did not have
// a desired value.
func EqualizeRequirementSpec(from, to *requirement.Unstructured) {

	// TODO(muvaf): This should include custom fields as well, which will probably
	// require a traversal of an unknown map
	toCurrent := requirement.New(func(u *requirement.Unstructured) {
		u.SetUnstructuredContent(to.GetUnstructured().DeepCopy().UnstructuredContent())
	})

	// The whole spec is copied blindly and then later we make the corrections
	// using toCurrent.
	spec, _ := fieldpath.Pave(from.GetUnstructured().UnstructuredContent()).GetValue("spec")
	_ = fieldpath.Pave(to.GetUnstructured().UnstructuredContent()).SetValue("spec", spec)
	switch {
	// We don't have an opinion about this field, so we late initialize its assigned
	// value.
	case from.GetCompositionSelector() == nil && toCurrent.GetCompositionSelector() != nil:
		from.SetCompositionSelector(toCurrent.GetCompositionSelector())
	// We do have an opinion about this field and it should override whatever is
	// returned.
	case from.GetCompositionSelector() != nil:
		to.SetCompositionSelector(from.GetCompositionSelector())
	}
	// In the end, both "to" and "from" has our most up-to-date desired state that
	// includes the fields that we didn't provide a value, i.e. late-inited.
	// The same logic goes for all Requirement fields.

	switch {
	case from.GetCompositionReference() == nil && toCurrent.GetCompositionReference() != nil:
		from.SetCompositionReference(toCurrent.GetCompositionReference())
	case from.GetCompositionReference() != nil:
		to.SetCompositionReference(from.GetCompositionReference())
	}
	switch {
	case from.GetResourceReference() == nil && toCurrent.GetResourceReference() != nil:
		from.SetResourceReference(toCurrent.GetResourceReference())
	case from.GetResourceReference() != nil:
		to.SetResourceReference(from.GetResourceReference())
	}
	switch {
	case from.GetWriteConnectionSecretToReference() == nil && toCurrent.GetWriteConnectionSecretToReference() != nil:
		from.SetWriteConnectionSecretToReference(toCurrent.GetWriteConnectionSecretToReference())
	case from.GetWriteConnectionSecretToReference() != nil:
		to.SetWriteConnectionSecretToReference(from.GetWriteConnectionSecretToReference())
	}
}

// PropagateStatus uses the requirement status fields on "from" and writes them
// to "to". It never removes the existing conditions.
func PropagateStatus(from, to *requirement.Unstructured) error {
	status, err := fieldpath.Pave(from.GetUnstructured().UnstructuredContent()).GetValue("status")
	if err != nil {
		return resource.Ignore(fieldpath.IsNotFound, err)
	}
	statusJSON, err := json.Marshal(status)
	if err != nil {
		return err
	}
	conditions := &v1alpha1.ConditionedStatus{}
	if err := json.Unmarshal(statusJSON, conditions); err != nil {
		return err
	}
	to.SetConditions(conditions.Conditions...)
	return nil
}
