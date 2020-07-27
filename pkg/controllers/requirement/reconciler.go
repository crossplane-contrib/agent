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
	"time"

	"github.com/pkg/errors"
	v1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/types"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/manager"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"

	"github.com/crossplane/crossplane-runtime/pkg/event"
	"github.com/crossplane/crossplane-runtime/pkg/logging"
	rresource "github.com/crossplane/crossplane-runtime/pkg/resource"
	"github.com/crossplane/crossplane-runtime/pkg/resource/unstructured"
	"github.com/crossplane/crossplane-runtime/pkg/resource/unstructured/requirement"

	"github.com/crossplane/agent/pkg/resource"
)

const (
	timeout   = 2 * time.Minute
	longWait  = 1 * time.Minute
	shortWait = 30 * time.Second
)

func SetupRequirement(mgr manager.Manager, remoteClient client.Client, gvk schema.GroupVersionKind, logger logging.Logger) error {
	name := gvk.GroupKind().String()
	r := NewReconciler(mgr, remoteClient, gvk, logger)

	return ctrl.NewControllerManagedBy(mgr).
		Named(name).
		For(requirement.New(requirement.WithGroupVersionKind(gvk)).GetUnstructured()).
		Complete(r)
}

func NewReconciler(mgr manager.Manager, remoteClient client.Client, gvk schema.GroupVersionKind, logger logging.Logger) *Reconciler {
	ni := func() *requirement.Unstructured { return requirement.New(requirement.WithGroupVersionKind(gvk)) }
	lc := unstructured.NewClient(mgr.GetClient())
	rc := unstructured.NewClient(remoteClient)
	return &Reconciler{
		mgr: mgr,
		local: rresource.ClientApplicator{
			Client:     lc,
			Applicator: rresource.NewAPIUpdatingApplicator(lc),
		},
		remote: rresource.ClientApplicator{
			Client:     rc,
			Applicator: rresource.NewAPIUpdatingApplicator(rc),
		},
		newInstance: ni,
		log:         logger,
		record:      event.NewAPIRecorder(mgr.GetEventRecorderFor(gvk.GroupKind().String())),
	}
}

type Reconciler struct {
	mgr    ctrl.Manager
	local  rresource.ClientApplicator
	remote rresource.ClientApplicator

	newInstance func() *requirement.Unstructured

	log    logging.Logger
	record event.Recorder
}

func (r *Reconciler) Reconcile(req reconcile.Request) (reconcile.Result, error) {
	log := r.log.WithValues("request", req)
	log.Debug("Reconciling")

	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()

	re := r.newInstance()
	if err := r.local.Get(ctx, req.NamespacedName, re); err != nil {
		return reconcile.Result{RequeueAfter: shortWait}, errors.Wrap(err, "cannot get requirement in the local cluster")
	}
	reRemote := r.newInstance()
	err := r.remote.Get(ctx, req.NamespacedName, reRemote)
	if rresource.IgnoreNotFound(err) != nil {
		return reconcile.Result{RequeueAfter: shortWait}, errors.Wrap(err, "cannot get requirement in the remote cluster")
	}
	// Update the remote object with latest desired state.
	resource.EqualizeRequirementSpec(re, reRemote)
	if err := r.remote.Apply(ctx, reRemote); err != nil {
		return reconcile.Result{RequeueAfter: shortWait}, errors.Wrap(err, "cannot update requirement in the remote cluster")
	}
	// TODO(muvaf): Update local object only if it's changed after late-init.
	if err := r.local.Update(ctx, re); err != nil {
		return reconcile.Result{RequeueAfter: shortWait}, errors.Wrap(err, "cannot update requirement in the local cluster")
	}

	// Update the local object with latest observation.
	if err := resource.PropagateStatus(reRemote, re); err != nil {
		return reconcile.Result{RequeueAfter: shortWait}, errors.Wrap(err, "cannot propagate status of the requirement to the requirement in the local cluster")
	}
	if err := r.local.Status().Update(ctx, re); err != nil {
		return reconcile.Result{RequeueAfter: shortWait}, errors.Wrap(err, "cannot update status of requirement in the local cluster")
	}
	if re.GetWriteConnectionSecretToReference() == nil {
		return reconcile.Result{RequeueAfter: shortWait}, nil
	}

	// Update the connection secret.
	rs := &v1.Secret{}
	rnn := types.NamespacedName{
		Name:      reRemote.GetWriteConnectionSecretToReference().Name,
		Namespace: reRemote.GetNamespace(),
	}
	if err := r.remote.Get(ctx, rnn, rs); err != nil {
		return reconcile.Result{RequeueAfter: shortWait}, errors.Wrap(err, "cannot get secret of the requirement from the remote cluster")
	}
	ls := &v1.Secret{}
	lnn := types.NamespacedName{
		Name:      re.GetWriteConnectionSecretToReference().Name,
		Namespace: re.GetNamespace(),
	}
	if err := r.local.Get(ctx, lnn, ls); err != nil {
		return reconcile.Result{RequeueAfter: shortWait}, errors.Wrap(err, "cannot get secret of the requirement from the local cluster")
	}
	resource.OverrideMetadata(ls, rs)
	rs.SetNamespace(re.GetNamespace())
	if err := r.local.Apply(ctx, rs); err != nil {
		return reconcile.Result{RequeueAfter: shortWait}, errors.Wrap(err, "cannot update secret of the requirement in the local cluster")
	}
	return reconcile.Result{RequeueAfter: longWait}, nil
}
