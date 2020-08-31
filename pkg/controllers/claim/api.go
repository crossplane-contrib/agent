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

package claim

import (
	"context"

	"github.com/pkg/errors"
	v1 "k8s.io/api/core/v1"
	kerrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/types"

	"k8s.io/apimachinery/pkg/util/json"
	"sigs.k8s.io/controller-runtime/pkg/client"

	"github.com/crossplane/crossplane-runtime/apis/core/v1alpha1"
	"github.com/crossplane/crossplane-runtime/pkg/fieldpath"
	"github.com/crossplane/crossplane-runtime/pkg/meta"
	rresource "github.com/crossplane/crossplane-runtime/pkg/resource"
	"github.com/crossplane/crossplane-runtime/pkg/resource/unstructured/claim"

	"github.com/crossplane/agent/pkg/resource"
)

// PropagateFn is used to construct a Propagator with a bare function.
type PropagateFn func(ctx context.Context, local, remote *claim.Unstructured) error

// Propagate calls the supplied function.
func (p PropagateFn) Propagate(ctx context.Context, local, remote *claim.Unstructured) error {
	return p(ctx, local, remote)
}

// NewPropagatorChain returns a new PropagatorChain.
func NewPropagatorChain(p ...Propagator) PropagatorChain {
	return PropagatorChain(p)
}

// PropagatorChain calls Propagate method of all of its Propagators in order.
type PropagatorChain []Propagator

// Propagate calls all Propagate functions one by one.
func (pp PropagatorChain) Propagate(ctx context.Context, local, remote *claim.Unstructured) error {
	for _, p := range pp {
		if err := p.Propagate(ctx, local, remote); err != nil {
			return err
		}
	}
	return nil
}

// NewSpecPropagator returns a new SpecPropagator.
func NewSpecPropagator(kube rresource.ClientApplicator) *SpecPropagator {
	return &SpecPropagator{remoteClient: kube}
}

// SpecPropagator blindly propagates all spec fields of "from" object to
// "to" object.
type SpecPropagator struct {
	remoteClient rresource.ClientApplicator
}

// Propagate copies spec from one object to the other.
func (sp *SpecPropagator) Propagate(ctx context.Context, local, remote *claim.Unstructured) error {
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
	return errors.Wrap(sp.remoteClient.Apply(ctx, remote), remotePrefix+errApplyRequirement)
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

// Propagate copies the values from observed to desired if that field is empty in
// desired object.
func (li *LateInitializer) Propagate(ctx context.Context, local, remote *claim.Unstructured) error {
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
	return errors.Wrap(li.localClient.Update(ctx, local), localPrefix+errUpdateRequirement)
}

// NewStatusPropagator returns a new StatusPropagator.
func NewStatusPropagator() *StatusPropagator {
	return &StatusPropagator{}
}

// StatusPropagator propagates the status from the second object to the first one.
type StatusPropagator struct{}

// Propagate copies the status of remote object into local object.
func (sp *StatusPropagator) Propagate(ctx context.Context, local, remote *claim.Unstructured) error {
	status, err := fieldpath.Pave(remote.GetUnstructured().UnstructuredContent()).GetValue("status")
	if err != nil {
		return rresource.Ignore(fieldpath.IsNotFound, err)
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
	return nil
}

// NewConnectionSecretPropagator returns a new *ConnectionSecretPropagator.
func NewConnectionSecretPropagator(local, remote rresource.ClientApplicator) *ConnectionSecretPropagator {
	return &ConnectionSecretPropagator{localClient: local, remoteClient: remote}
}

// ConnectionSecretPropagator fetches the connection secret from the remote cluster
// and applies it in the local cluster.
type ConnectionSecretPropagator struct {
	localClient  rresource.ClientApplicator
	remoteClient rresource.ClientApplicator
}

// Propagate propagates the connection secret from remote cluster to local cluster.
func (csp *ConnectionSecretPropagator) Propagate(ctx context.Context, local, remote *claim.Unstructured) error {
	if local.GetWriteConnectionSecretToReference() == nil || remote.GetWriteConnectionSecretToReference() == nil {
		return nil
	}
	// Update the connection secret.
	rs := &v1.Secret{}
	rnn := types.NamespacedName{
		Name:      remote.GetWriteConnectionSecretToReference().Name,
		Namespace: remote.GetNamespace(),
	}
	err := csp.remoteClient.Get(ctx, rnn, rs)
	if rresource.IgnoreNotFound(err) != nil {
		return errors.Wrap(err, remotePrefix+errGetSecret)
	}
	if kerrors.IsNotFound(err) {
		// TODO(muvaf): Set condition to say waiting for secret.
		return nil
	}
	ls := resource.SanitizedDeepCopyObject(rs)
	ls.SetName(local.GetWriteConnectionSecretToReference().Name)
	ls.SetNamespace(local.GetNamespace())
	meta.AddOwnerReference(ls, meta.AsController(meta.ReferenceTo(local, local.GroupVersionKind())))
	if err := csp.localClient.Apply(ctx, ls); err != nil {
		return errors.Wrap(err, localPrefix+errApplySecret)
	}
	return nil
}
