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
	kerrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/types"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/manager"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"

	"github.com/crossplane/crossplane-runtime/pkg/event"
	"github.com/crossplane/crossplane-runtime/pkg/logging"
	"github.com/crossplane/crossplane-runtime/pkg/meta"
	rresource "github.com/crossplane/crossplane-runtime/pkg/resource"
	"github.com/crossplane/crossplane-runtime/pkg/resource/unstructured"
	"github.com/crossplane/crossplane-runtime/pkg/resource/unstructured/requirement"

	"github.com/crossplane/agent/pkg/resource"
)

const (
	timeout   = 2 * time.Minute
	longWait  = 1 * time.Minute
	shortWait = 30 * time.Second
	tinyWait  = 5 * time.Second

	finalizer = "agent.crossplane.io/sync"

	localPrefix  = "local cluster: "
	remotePrefix = "remote cluster: "

	errGetRequirement          = "cannot get requirement"
	errDeleteRequirement       = "cannot delete requirement"
	errApplyRequirement        = "cannot apply requirement"
	errUpdateRequirement       = "cannot update requirement"
	errStatusUpdateRequirement = "cannot update status of requirement"
	errRemoveFinalizer         = "cannot remove finalizer"
	errAddFinalizer            = "cannot add finalizer"
	errGetSecret               = "cannot get secret"
	errApplySecret             = "cannot apply secret"
)

// WithLogger specifies how the Reconciler should log messages.
func WithLogger(l logging.Logger) ReconcilerOption {
	return func(r *Reconciler) {
		r.log = l
	}
}

// WithRecorder specifies how the Reconciler should record events.
func WithRecorder(rec event.Recorder) ReconcilerOption {
	return func(r *Reconciler) {
		r.record = rec
	}
}

// WithFinalizer specifies how the Reconciler should add and remove finalizers.
func WithFinalizer(f rresource.Finalizer) ReconcilerOption {
	return func(r *Reconciler) {
		r.finalizer = f
	}
}

// ReconcilerOption is used to configure *Reconciler.
type ReconcilerOption func(*Reconciler)

// NewReconciler returns a new *Reconciler.
func NewReconciler(mgr manager.Manager, remoteClient client.Client, gvk schema.GroupVersionKind, opts ...ReconcilerOption) *Reconciler {
	ni := func() *requirement.Unstructured { return requirement.New(requirement.WithGroupVersionKind(gvk)) }
	lc := unstructured.NewClient(mgr.GetClient())
	rc := unstructured.NewClient(remoteClient)
	r := &Reconciler{
		mgr: mgr,
		local: rresource.ClientApplicator{
			Client:     lc,
			Applicator: rresource.NewAPIPatchingApplicator(lc),
		},
		remote: rresource.ClientApplicator{
			Client:     rc,
			Applicator: rresource.NewAPIPatchingApplicator(rc),
		},
		newInstance: ni,
		log:         logging.NewNopLogger(),
		finalizer:   rresource.NewAPIFinalizer(lc, finalizer),
		// NOTE(muvaf): Late init should be done first to not override existing
		// generated fields in the remote object such as resourceRef.
		requirement: ConfiguratorChain([]Configurator{
			NewMetadataPropagator(),
			NewLateInitializer(),
			NewSpecPropagator(),
			NewStatusConfigurator(),
		}),
		record: event.NewNopRecorder(),
	}

	for _, f := range opts {
		f(r)
	}
	return r
}

// Configurator is used configure local and remote objects.
type Configurator interface {
	Configure(local, remote *requirement.Unstructured) error
}

// Reconciler syncs the given requirement instance from local cluster to remote
// cluster and fetches its connection secret to local cluster if it's available.
type Reconciler struct {
	mgr    ctrl.Manager
	local  rresource.ClientApplicator
	remote rresource.ClientApplicator

	newInstance func() *requirement.Unstructured

	finalizer   rresource.Finalizer
	requirement Configurator

	log    logging.Logger
	record event.Recorder
}

// Reconcile watches the given type and does necessary sync operations.
func (r *Reconciler) Reconcile(req reconcile.Request) (reconcile.Result, error) {
	log := r.log.WithValues("request", req)
	log.Debug("Reconciling")

	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()

	local := r.newInstance()
	if err := r.local.Get(ctx, req.NamespacedName, local); err != nil {
		if kerrors.IsNotFound(err) {
			return reconcile.Result{Requeue: false}, nil
		}
		return reconcile.Result{RequeueAfter: shortWait}, errors.Wrap(err, localPrefix+errGetRequirement)
	}
	remote := r.newInstance()
	err := r.remote.Get(ctx, req.NamespacedName, remote)
	if rresource.IgnoreNotFound(err) != nil {
		return reconcile.Result{RequeueAfter: shortWait}, errors.Wrap(err, remotePrefix+errGetRequirement)
	}
	if meta.WasDeleted(local) {
		if kerrors.IsNotFound(err) {
			if err := r.finalizer.RemoveFinalizer(ctx, local); err != nil {
				return reconcile.Result{RequeueAfter: shortWait}, errors.Wrap(err, localPrefix+errRemoveFinalizer)
			}
			return reconcile.Result{}, nil
		}
		if err := r.remote.Delete(ctx, remote); rresource.IgnoreNotFound(err) != nil {
			return reconcile.Result{RequeueAfter: shortWait}, errors.Wrap(err, remotePrefix+errDeleteRequirement)
		}
		return reconcile.Result{RequeueAfter: tinyWait}, nil
	}

	if err := r.finalizer.AddFinalizer(ctx, local); err != nil {
		return reconcile.Result{RequeueAfter: shortWait}, errors.Wrap(err, localPrefix+errAddFinalizer)
	}

	// Update the remote object with latest desired state.
	if err := r.requirement.Configure(local, remote); err != nil {
		return reconcile.Result{RequeueAfter: shortWait}, errors.Wrap(err, remotePrefix+errApplyRequirement)
	}
	if err := r.remote.Apply(ctx, remote); err != nil {
		return reconcile.Result{RequeueAfter: shortWait}, errors.Wrap(err, remotePrefix+errApplyRequirement)
	}
	if err := r.local.Apply(ctx, local); err != nil {
		return reconcile.Result{RequeueAfter: shortWait}, errors.Wrap(err, localPrefix+errUpdateRequirement)
	}

	if local.GetWriteConnectionSecretToReference() == nil {
		return reconcile.Result{RequeueAfter: longWait}, nil
	}

	// Update the connection secret.
	rs := &v1.Secret{}
	rnn := types.NamespacedName{
		Name:      remote.GetWriteConnectionSecretToReference().Name,
		Namespace: remote.GetNamespace(),
	}
	err = r.remote.Get(ctx, rnn, rs)
	if rresource.IgnoreNotFound(err) != nil {
		return reconcile.Result{RequeueAfter: shortWait}, errors.Wrap(err, remotePrefix+errGetSecret)
	}
	if kerrors.IsNotFound(err) {
		// TODO(muvaf): Set condition to say waiting for secret.
		return reconcile.Result{RequeueAfter: shortWait}, nil
	}
	ls := resource.SanitizedDeepCopyObject(rs)
	ls.SetName(local.GetWriteConnectionSecretToReference().Name)
	ls.SetNamespace(local.GetNamespace())
	meta.AddOwnerReference(ls, meta.AsController(meta.ReferenceTo(local, local.GroupVersionKind())))
	if err := r.local.Apply(ctx, ls); err != nil {
		return reconcile.Result{RequeueAfter: shortWait}, errors.Wrap(err, localPrefix+errApplySecret)
	}
	return reconcile.Result{RequeueAfter: longWait}, nil
}
