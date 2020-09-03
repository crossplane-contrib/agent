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
	"time"

	"github.com/pkg/errors"
	kerrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/runtime/schema"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/manager"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"

	"github.com/crossplane/crossplane-runtime/pkg/event"
	"github.com/crossplane/crossplane-runtime/pkg/logging"
	"github.com/crossplane/crossplane-runtime/pkg/meta"
	rresource "github.com/crossplane/crossplane-runtime/pkg/resource"
	"github.com/crossplane/crossplane-runtime/pkg/resource/unstructured"
	"github.com/crossplane/crossplane-runtime/pkg/resource/unstructured/claim"

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

	errGetRequirement    = "cannot get claim"
	errDeleteClaim       = "cannot delete claim"
	errApplyClaim        = "cannot apply claim"
	errPush              = "cannot run push propagator"
	errPull              = "cannot run pull propagator"
	errUpdateClaim       = "cannot update claim"
	errStatusUpdateClaim = "cannot update status of claim"
	errRemoveFinalizer   = "cannot remove finalizer"
	errAddFinalizer      = "cannot add finalizer"
	errGetSecret         = "cannot get secret"
	errApplySecret       = "cannot apply secret"
)

// Event reasons.
const (
	reasonCannotGetFromRemote   event.Reason = "CannotGetFromRemote"
	reasonCannotAddFinalizer    event.Reason = "CannotAddFinalizer"
	reasonCannotRemoveFinalizer event.Reason = "CannotRemoveFinalizer"
	reasonCannotConfigure       event.Reason = "CannotConfigure"
	reasonCannotApply           event.Reason = "CannotApply"
	reasonCannotPropagate       event.Reason = "CannotPropagate"
	reasonCannotDelete          event.Reason = "CannotDelete"
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

// WithPropagator specifies how the Reconciler should propagate values and objects
// between clusters.
func WithPropagator(p Propagator) ReconcilerOption {
	return func(r *Reconciler) {
		r.Propagator = p
	}
}

// ReconcilerOption is used to configure *Reconciler.
type ReconcilerOption func(*Reconciler)

// NewReconciler returns a new *Reconciler.
func NewReconciler(mgr manager.Manager, remoteClient client.Client, gvk schema.GroupVersionKind, opts ...ReconcilerOption) *Reconciler {
	ni := func() *claim.Unstructured { return claim.New(claim.WithGroupVersionKind(gvk)) }
	lc := unstructured.NewClient(mgr.GetClient())
	lca := rresource.ClientApplicator{
		Client:     lc,
		Applicator: rresource.NewAPIPatchingApplicator(lc),
	}
	rc := unstructured.NewClient(remoteClient)
	rca := rresource.ClientApplicator{
		Client:     rc,
		Applicator: rresource.NewAPIPatchingApplicator(rc),
	}
	r := &Reconciler{
		mgr:          mgr,
		local:        lca,
		remote:       rca,
		newInstance:  ni,
		log:          logging.NewNopLogger(),
		finalizer:    rresource.NewAPIFinalizer(lc, finalizer),
		Configurator: NewDefaultConfigurator(),
		Propagator: NewPropagatorChain(
			NewLateInitializer(lc),
			NewStatusPropagator(),
			NewConnectionSecretPropagator(lca, rca),
		),
		record: event.NewNopRecorder(),
	}

	for _, f := range opts {
		f(r)
	}
	return r
}

// Configurator configures the supplied remote instance.
type Configurator interface {
	Configure(ctx context.Context, local, remote *claim.Unstructured) error
}

// Propagator is used to propagate values from remote to the local object.
type Propagator interface {
	Propagate(ctx context.Context, local, remote *claim.Unstructured) error
}

// Reconciler syncs the given claim instance from local cluster to remote
// cluster and fetches its connection secret to local cluster if it's available.
type Reconciler struct {
	mgr    ctrl.Manager
	local  rresource.ClientApplicator
	remote rresource.ClientApplicator

	newInstance func() *claim.Unstructured

	finalizer rresource.Finalizer
	Configurator
	Propagator

	log    logging.Logger
	record event.Recorder
}

// Reconcile watches the given type and does necessary sync operations.
func (r *Reconciler) Reconcile(req reconcile.Request) (reconcile.Result, error) { // nolint:gocyclo
	log := r.log.WithValues("request", req)
	log.Debug("Reconciling")

	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()

	// The reconciliation is triggered for the local claim instance, so, if it
	// cannot be fetched for any reason, then that's a problem.
	localClaim := r.newInstance()
	if err := r.local.Get(ctx, req.NamespacedName, localClaim); err != nil {
		if kerrors.IsNotFound(err) {
			return reconcile.Result{Requeue: false}, nil
		}
		return reconcile.Result{RequeueAfter: shortWait}, errors.Wrap(err, localPrefix+errGetRequirement)
	}

	// We fetch the remote claim instance that corresponds to this one and ignore
	// the NotFound error since this pass could be the first one where the remote
	// instance will be created.
	remoteClaim := r.newInstance()
	err := r.remote.Get(ctx, req.NamespacedName, remoteClaim)
	if rresource.IgnoreNotFound(err) != nil {
		log.Debug("Cannot get resource from remote", "error", err, "requeue-after", time.Now().Add(shortWait))
		r.record.Event(localClaim, event.Warning(reasonCannotGetFromRemote, err))
		localClaim.SetConditions(resource.AgentSyncError(errors.Wrap(err, remotePrefix+errGetRequirement)))
		return reconcile.Result{RequeueAfter: shortWait}, errors.Wrap(r.local.Status().Update(ctx, localClaim), errStatusUpdateClaim)
	}

	// If local claim instance is deleted, we need to clean up the remote instance
	// before allowing it to disappear from api-server.
	if meta.WasDeleted(localClaim) {

		// If the remote instance is already gone, then there is nothing else we
		// need to clean up. The connection secret we created will be deleted by
		// api-server once local instance is gone since we added our owner ref
		// to it.
		if kerrors.IsNotFound(err) {
			if err := r.finalizer.RemoveFinalizer(ctx, localClaim); err != nil {
				log.Debug("Cannot remove finalizer", "error", err, "requeue-after", time.Now().Add(shortWait))
				r.record.Event(localClaim, event.Warning(reasonCannotRemoveFinalizer, err))
				localClaim.SetConditions(resource.AgentSyncError(errors.Wrap(err, localPrefix+errRemoveFinalizer)))
				return reconcile.Result{RequeueAfter: shortWait}, errors.Wrap(r.local.Status().Update(ctx, localClaim), errStatusUpdateClaim)
			}
			return reconcile.Result{}, nil
		}

		// Start the deletion of remote instance and if it's already gone, that's
		// not an error since that's what we'd like to achieve.
		if err := r.remote.Delete(ctx, remoteClaim); rresource.IgnoreNotFound(err) != nil {
			log.Debug("Cannot delete local object", "error", err, "requeue-after", time.Now().Add(shortWait))
			r.record.Event(localClaim, event.Warning(reasonCannotDelete, err))
			localClaim.SetConditions(resource.AgentSyncError(errors.Wrap(err, remotePrefix+errDeleteClaim)))
			return reconcile.Result{RequeueAfter: shortWait}, errors.Wrap(r.local.Status().Update(ctx, localClaim), errStatusUpdateClaim)
		}

		// We have requested the deletion of the remote instance but that doesn't
		// meant it's gone. So, we'll requeue and remove the finalizer only if we
		// confirm that remote instance no longer exists.
		localClaim.SetConditions(resource.AgentSyncSuccess().WithMessage("Deletion is successfully requested"))
		return reconcile.Result{RequeueAfter: tinyWait}, errors.Wrap(r.local.Status().Update(ctx, localClaim), errStatusUpdateClaim)
	}

	// At this point, we will begin the operations that will need some cleanup in
	// case of deletion, such as creation of remote correspondent. So, we add to a
	// finalizer to local claim instance to block its deletion until this controller
	// takes care of the cleanup.
	if err := r.finalizer.AddFinalizer(ctx, localClaim); err != nil {
		log.Debug("Cannot add finalizer", "error", err, "requeue-after", time.Now().Add(shortWait))
		r.record.Event(localClaim, event.Warning(reasonCannotAddFinalizer, err))
		localClaim.SetConditions(resource.AgentSyncError(errors.Wrap(err, localPrefix+errAddFinalizer)))
		return reconcile.Result{RequeueAfter: shortWait}, errors.Wrap(r.local.Status().Update(ctx, localClaim), errStatusUpdateClaim)
	}

	// At this point, we are getting remote instance ready for Apply operation
	// by configuring its fields.
	if err := r.Configure(ctx, localClaim, remoteClaim); err != nil {
		log.Debug("Cannot run configurator", "error", err, "requeue-after", time.Now().Add(shortWait))
		r.record.Event(localClaim, event.Warning(reasonCannotConfigure, err))
		localClaim.SetConditions(resource.AgentSyncError(errors.Wrap(err, errPush)))
		return reconcile.Result{RequeueAfter: shortWait}, errors.Wrap(r.local.Status().Update(ctx, localClaim), errStatusUpdateClaim)
	}

	// We create/update the final form of the instance in the remote cluster.
	if err := r.remote.Apply(ctx, remoteClaim); err != nil {
		log.Debug("Cannot call Apply", "error", err, "requeue-after", time.Now().Add(shortWait))
		r.record.Event(localClaim, event.Warning(reasonCannotApply, err))
		localClaim.SetConditions(resource.AgentSyncError(errors.Wrap(err, errApplyClaim)))
		return reconcile.Result{RequeueAfter: shortWait}, errors.Wrap(r.local.Status().Update(ctx, localClaim), errStatusUpdateClaim)
	}

	// At this point, we have the remote instance in the remote cluster and the
	// variable "remote" is updated. So, we will propagate new information from
	// "remote" to "local"
	if err := r.Propagate(ctx, localClaim, remoteClaim); err != nil {
		log.Debug("Cannot run propagator", "error", err, "requeue-after", time.Now().Add(shortWait))
		r.record.Event(localClaim, event.Warning(reasonCannotPropagate, err))
		localClaim.SetConditions(resource.AgentSyncError(errors.Wrap(err, errPull)))
		return reconcile.Result{RequeueAfter: shortWait}, errors.Wrap(r.local.Status().Update(ctx, localClaim), errStatusUpdateClaim)
	}
	localClaim.SetConditions(resource.AgentSyncSuccess())
	return reconcile.Result{RequeueAfter: longWait}, errors.Wrap(r.local.Status().Update(ctx, localClaim), localPrefix+errStatusUpdateClaim)
}
