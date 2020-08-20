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

package requirement

import (
	"k8s.io/apimachinery/pkg/util/json"

	"github.com/crossplane/crossplane-runtime/apis/core/v1alpha1"
	"github.com/crossplane/crossplane-runtime/pkg/fieldpath"
	"github.com/crossplane/crossplane-runtime/pkg/resource"
	"github.com/crossplane/crossplane-runtime/pkg/resource/unstructured/requirement"
)

// ConfiguratorChain calls Configure method of all of its Configurators in order.
type ConfiguratorChain []Configurator

// Configure calls all Configure objects one by one.
func (cc ConfiguratorChain) Configure(local, remote *requirement.Unstructured) error {
	for _, c := range cc {
		if err := c.Configure(local, remote); err != nil {
			return err
		}
	}
	return nil
}

// NewMetadataPropagator returns a new MetadataPropagator.
func NewMetadataPropagator() MetadataPropagator {
	return MetadataPropagator{}
}

// MetadataPropagator copies the desired state which includes spec fields and
// some of the metadata fields.
type MetadataPropagator struct{}

// Configure updates "to" object's user-entered metadata.
func (dc MetadataPropagator) Configure(from, to *requirement.Unstructured) error {
	to.SetName(from.GetName())
	to.SetNamespace(from.GetNamespace())
	to.SetAnnotations(from.GetAnnotations())
	to.SetLabels(from.GetLabels())
	return nil
}

// NewSpecPropagator returns a new SpecPropagator.
func NewSpecPropagator() SpecPropagator {
	return SpecPropagator{}
}

// SpecPropagator blindly propagates all spec fields of "from" object to
// "to" object.
type SpecPropagator struct{}

// Configure copies spec from one object to the other.
func (dc SpecPropagator) Configure(from, to *requirement.Unstructured) error {
	spec, err := fieldpath.Pave(from.GetUnstructured().UnstructuredContent()).GetValue("spec")
	if err != nil {
		return err
	}
	err = fieldpath.Pave(to.GetUnstructured().UnstructuredContent()).SetValue("spec", spec)
	return err
}

// NewLateInitializer returns a new LateInitializer.
func NewLateInitializer() LateInitializer {
	return LateInitializer{}
}

// LateInitializer fills up the empty fields of "desired" object with the values
// in "observed" object.
type LateInitializer struct{}

// Configure copies the values from observed to desired if that field is empty in
// desired object.
func (dc LateInitializer) Configure(desired, observed *requirement.Unstructured) error {
	// We fill up the missing pieces in our desired state by late initializing.
	if desired.GetCompositionSelector() == nil {
		desired.SetCompositionSelector(observed.GetCompositionSelector())
	}
	if desired.GetCompositionReference() == nil {
		desired.SetCompositionReference(observed.GetCompositionReference())
	}
	if desired.GetResourceReference() == nil {
		desired.SetResourceReference(observed.GetResourceReference())
	}
	if desired.GetWriteConnectionSecretToReference() == nil {
		desired.SetWriteConnectionSecretToReference(observed.GetWriteConnectionSecretToReference())
	}
	// TODO(muvaf): We need to late-init the unknown user-defined fields as well.
	return nil
}

// NewStatusConfigurator returns a new StatusPropagator.
func NewStatusConfigurator() StatusPropagator {
	return StatusPropagator{}
}

// StatusPropagator propagates the status from the second object to the first one.
type StatusPropagator struct{}

// Configure copies the status of remote object into local object.
func (dc StatusPropagator) Configure(local, remote *requirement.Unstructured) error {
	status, err := fieldpath.Pave(remote.GetUnstructured().UnstructuredContent()).GetValue("status")
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
	local.SetConditions(conditions.Conditions...)
	// TODO(muvaf): Need to propagate other feilds as well.
	return nil
}
