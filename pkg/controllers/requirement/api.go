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
	"context"

	"k8s.io/apimachinery/pkg/util/json"
	"sigs.k8s.io/controller-runtime/pkg/client"

	"github.com/crossplane/crossplane-runtime/apis/core/v1alpha1"
	"github.com/crossplane/crossplane-runtime/pkg/fieldpath"
	"github.com/crossplane/crossplane-runtime/pkg/resource"
	"github.com/crossplane/crossplane-runtime/pkg/resource/unstructured/requirement"
)

// NewPropagatorChain returns a new PropagatorChain.
func NewPropagatorChain(p ...Propagator) PropagatorChain {
	return PropagatorChain(p)
}

// PropagatorChain calls Propagate method of all of its Propagators in order.
type PropagatorChain []Propagator

// Propagate calls all Propagate functions one by one.
func (pp PropagatorChain) Propagate(ctx context.Context, local, remote *requirement.Unstructured) error {
	for _, p := range pp {
		if err := p.Propagate(ctx, local, remote); err != nil {
			return err
		}
	}
	return nil
}

// NewSpecPropagator returns a new SpecPropagator.
func NewSpecPropagator(kube resource.ClientApplicator) *SpecPropagator {
	return &SpecPropagator{remoteClient: kube}
}

// SpecPropagator blindly propagates all spec fields of "from" object to
// "to" object.
type SpecPropagator struct {
	remoteClient resource.ClientApplicator
}

// Configure copies spec from one object to the other.
func (sp *SpecPropagator) Propagate(ctx context.Context, local, remote *requirement.Unstructured) error {
	remote.SetName(local.GetName())
	remote.SetNamespace(local.GetNamespace())
	remote.SetAnnotations(local.GetAnnotations())
	remote.SetLabels(local.GetLabels())
	spec, err := fieldpath.Pave(local.GetUnstructured().UnstructuredContent()).GetValue("spec")
	if err != nil {
		return err
	}
	if err := fieldpath.Pave(remote.GetUnstructured().UnstructuredContent()).SetValue("spec", spec); err != nil {
		return err
	}
	return sp.remoteClient.Apply(ctx, remote)
}

// NewLateInitializer returns a new LateInitializer.
func NewLateInitializer(kube client.Client) *LateInitializer {
	return &LateInitializer{localClient: kube}
}

// LateInitializer fills up the empty fields of "desired" object with the values
// in "observed" object.
type LateInitializer struct {
	localClient client.Client
}

// Configure copies the values from observed to desired if that field is empty in
// desired object.
func (li *LateInitializer) Propagate(ctx context.Context, local, remote *requirement.Unstructured) error {
	// We fill up the missing pieces in our desired state by late initializing.
	if local.GetCompositionSelector() == nil && remote.GetCompositionSelector() != nil {
		local.SetCompositionSelector(remote.GetCompositionSelector())
	}
	if local.GetCompositionReference() == nil && remote.GetCompositionReference() != nil {
		local.SetCompositionReference(remote.GetCompositionReference())
	}
	if local.GetResourceReference() == nil && remote.GetResourceReference() != nil {
		local.SetResourceReference(remote.GetResourceReference())
	}
	if local.GetWriteConnectionSecretToReference() == nil && remote.GetWriteConnectionSecretToReference() != nil {
		local.SetWriteConnectionSecretToReference(remote.GetWriteConnectionSecretToReference())
	}
	// TODO(muvaf): We need to late-init the unknown user-defined fields as well.
	return li.localClient.Update(ctx, local)
}

// NewStatusPropagator returns a new StatusPropagator.
func NewStatusPropagator(kube client.Client) *StatusPropagator {
	return &StatusPropagator{localClient: kube}
}

// StatusPropagator propagates the status from the second object to the first one.
type StatusPropagator struct {
	localClient client.Client
}

// Configure copies the status of remote object into local object.
func (sp *StatusPropagator) Propagate(ctx context.Context, local, remote *requirement.Unstructured) error {
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
	// TODO(muvaf): Need to propagate other fields as well.
	return sp.localClient.Status().Update(ctx, local)
}
