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

package xrd

import (
	"context"
	"time"

	"github.com/crossplane/agent/pkg/resource"

	"github.com/pkg/errors"
	"k8s.io/apiextensions-apiserver/pkg/apis/apiextensions/v1beta1"
	kmeta "k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	kunstructured "k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	kcontroller "sigs.k8s.io/controller-runtime/pkg/controller"
	"sigs.k8s.io/controller-runtime/pkg/handler"
	"sigs.k8s.io/controller-runtime/pkg/manager"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"

	runtimev1alpha1 "github.com/crossplane/crossplane-runtime/apis/core/v1alpha1"
	"github.com/crossplane/crossplane-runtime/pkg/controller"
	"github.com/crossplane/crossplane-runtime/pkg/event"
	"github.com/crossplane/crossplane-runtime/pkg/logging"
	"github.com/crossplane/crossplane-runtime/pkg/meta"
	rresource "github.com/crossplane/crossplane-runtime/pkg/resource"
	"github.com/crossplane/crossplane/apis/apiextensions/v1alpha1"
	"github.com/crossplane/crossplane/apis/apiextensions/v1alpha1/ccrd"
	cclaim "github.com/crossplane/crossplane/pkg/controller/apiextensions/claim"

	"github.com/crossplane/agent/pkg/controllers/claim"
)

const (
	timeout   = 2 * time.Minute
	longWait  = 1 * time.Minute
	shortWait = 30 * time.Second
	tinyWait  = 3 * time.Second

	finalizer = "agent.crossplane.io/claim-crd-controller"

	localPrefix        = "local cluster: "
	remotePrefix       = "remote cluster: "
	errUpdateStatus    = "cannot update status of xrd"
	errStartController = "cannot start controller"
	errRemoveFinalizer = "cannot remove finalizer"
	errGetXRD          = "cannot get xrd"
	errFetchCRD        = "cannot fetch the crd of xrd from remote"
	errGetCRD          = "cannot get custom resource definition"
	errApplyCRD        = "cannot apply custom resource definition"
	errListCR          = "cannot list custom resources of claim type"
	errDeleteCR        = "cannot delete custom resources of claim type"
	errDeleteCRD       = "cannot delete crd of claim type"
	errAddFinalizerXRD = "cannot add finalizer to xrd"
)

// Setup adds a controller that will reconcile CompositeResourceDefinitions that
// offer resource claim in the local cluster and create CRDs & controllers that
// will reconcile those new types.
func Setup(mgr manager.Manager, remoteClient client.Client, logger logging.Logger) error {
	name := "ClaimCustomResourceDefinitions"
	r := NewReconciler(mgr, remoteClient,
		WithCRDFetcher(NewAPIRemoteCRDFetcher(remoteClient)),
		WithLogger(logger),
		WithRecorder(event.NewAPIRecorder(mgr.GetEventRecorderFor(name))))
	return ctrl.NewControllerManagedBy(mgr).
		Named(name).
		For(&v1alpha1.CompositeResourceDefinition{}).
		WithEventFilter(resource.NewXRDWithClaim()).
		Owns(&v1beta1.CustomResourceDefinition{}).
		Complete(r)
}

// WithControllerEngine specifies how the Reconciler should start and stop controllers.
func WithControllerEngine(c ControllerEngine) ReconcilerOption {
	return func(r *Reconciler) {
		r.engine = c
	}
}

// WithFinalizer specifies how the Reconciler should add and remove finalizers.
func WithFinalizer(f rresource.Finalizer) ReconcilerOption {
	return func(r *Reconciler) {
		r.finalizer = f
	}
}

// WithCRDFetcher specifies how the Reconciler should fetch CRDs of claims.
func WithCRDFetcher(re CRDFetcher) ReconcilerOption {
	return func(r *Reconciler) {
		r.crd = re
	}
}

// WithLocalApplicator specifies what Applicator in local cluster Reconciler
// should use.
func WithLocalApplicator(a rresource.Applicator) ReconcilerOption {
	return func(r *Reconciler) {
		r.local.Applicator = a
	}
}

// WithLogger specifies how the Reconciler should log messages.
func WithLogger(log logging.Logger) ReconcilerOption {
	return func(r *Reconciler) {
		r.log = log
	}
}

// WithRecorder specifies how the Reconciler should record Kubernetes events.
func WithRecorder(er event.Recorder) ReconcilerOption {
	return func(r *Reconciler) {
		r.record = er
	}
}

// ReconcilerOption is used to configure *Reconciler.
type ReconcilerOption func(*Reconciler)

// NewReconciler returns a new *Reconciler.
func NewReconciler(mgr manager.Manager, remoteClient client.Client, opts ...ReconcilerOption) *Reconciler {
	r := &Reconciler{
		mgr: mgr,
		local: rresource.ClientApplicator{
			Client:     mgr.GetClient(),
			Applicator: rresource.NewAPIUpdatingApplicator(mgr.GetClient()),
		},
		remote:    remoteClient,
		engine:    controller.NewEngine(mgr),
		crd:       NewNopFetcher(),
		finalizer: rresource.NewAPIFinalizer(mgr.GetClient(), finalizer),
		log:       logging.NewNopLogger(),
		record:    event.NewNopRecorder(),
	}
	for _, f := range opts {
		f(r)
	}
	return r
}

// ControllerEngine can be satisfied with objects that can start and stop a controller.
type ControllerEngine interface {
	Start(name string, o kcontroller.Options, w ...controller.Watch) error
	Stop(name string)
}

// CRDFetcher can be satisfied with objects that can return a CRD with
// CompositeResourceDefinition information.
type CRDFetcher interface {
	Fetch(ctx context.Context, ip v1alpha1.CompositeResourceDefinition) (*v1beta1.CustomResourceDefinition, error)
}

// Reconciler watches the CompositeResourceDefinition with resource claim offerings
// in the cluster and creates a CRD for each of them with spec that is fetched
// via supplied CRDFetcher. Then it creates a controller for each new type that
// will sync the instances of that type from local cluster to remote cluster.
type Reconciler struct {
	mgr    ctrl.Manager
	local  rresource.ClientApplicator
	remote client.Client

	crd       CRDFetcher
	engine    ControllerEngine
	finalizer rresource.Finalizer

	log    logging.Logger
	record event.Recorder
}

// TODO(muvaf): Set error conditions on the CompositeResourceDefinition.

// Reconcile reconciles CompositeResourceDefinition and does the necessary operations
// to bootstrap reconciliation of that new type defined by CompositeResourceDefinition.
func (r *Reconciler) Reconcile(req reconcile.Request) (reconcile.Result, error) { // nolint:gocyclo
	log := r.log.WithValues("request", req)
	log.Debug("Reconciling")

	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()

	xrd := &v1alpha1.CompositeResourceDefinition{}
	if err := r.local.Get(ctx, req.NamespacedName, xrd); rresource.IgnoreNotFound(err) != nil {
		return reconcile.Result{RequeueAfter: shortWait}, errors.Wrap(err, localPrefix+errGetXRD)
	}

	// We will fetch the CRD of the claim that CompositeResourceDefinition offers
	// and apply it in the local cluster so that we can start the sync controller
	// targeting that type.
	local, err := r.crd.Fetch(ctx, *xrd)
	if rresource.IgnoreNotFound(err) != nil {
		return reconcile.Result{RequeueAfter: shortWait}, errors.Wrap(err, remotePrefix+errFetchCRD)
	}

	// In case XRD is deleted, we need to clean up the CRD and stop its controller.
	if meta.WasDeleted(xrd) {
		xrd.Status.SetConditions(v1alpha1.Deleting())
		err := r.local.Get(ctx, GetClaimCRDName(*xrd), local)
		if rresource.IgnoreNotFound(err) != nil {
			return reconcile.Result{RequeueAfter: shortWait}, errors.Wrap(err, localPrefix+errGetCRD)
		}

		// The CRD has no creation timestamp, or we don't control it. Most
		// likely we successfully deleted it on a previous reconcile. It's also
		// possible that we're being asked to delete it before we got around to
		// creating it, or that we lost control of it around the same time we
		// were deleted. In the (presumably exceedingly rare) latter case we'll
		// orphan the CRD.
		if !meta.WasCreated(local) || !metav1.IsControlledBy(local, xrd) {
			// It's likely that we've already stopped this controller on a
			// previous reconcile, but we try again just in case. This is a
			// no-op if the controller was already stopped.
			r.engine.Stop(cclaim.ControllerName(xrd.GetName()))

			if err := r.finalizer.RemoveFinalizer(ctx, xrd); err != nil {
				return reconcile.Result{RequeueAfter: shortWait}, errors.Wrap(err, localPrefix+errRemoveFinalizer)
			}

			// We're all done deleting and have removed our finalizer. There's
			// no need to requeue because there's nothing left to do.
			return reconcile.Result{Requeue: false}, nil
		}

		l := &kunstructured.UnstructuredList{}
		l.SetGroupVersionKind(GroupVersionKindOf(*local))
		if err := r.local.List(ctx, l); rresource.Ignore(kmeta.IsNoMatchError, err) != nil {
			return reconcile.Result{RequeueAfter: shortWait}, errors.Wrap(err, localPrefix+errListCR)
		}

		// Ensure all the custom resources we defined are gone before stopping
		// the controller we started to reconcile them. This ensures the
		// controller has a chance to execute its cleanup logic, if any.
		if len(l.Items) > 0 {
			for i := range l.Items {
				if err := r.local.Delete(ctx, &l.Items[i]); rresource.IgnoreNotFound(err) != nil {
					return reconcile.Result{RequeueAfter: shortWait}, errors.Wrap(err, localPrefix+errDeleteCR)
				}
			}

			// We requeue to confirm that all the custom resources we just
			// deleted are actually gone. We need to requeue after a tiny wait
			// because we won't be requeued implicitly when the CRs are deleted.
			return reconcile.Result{RequeueAfter: tinyWait}, errors.Wrap(r.local.Status().Update(ctx, xrd), localPrefix+errUpdateStatus)
		}

		// The controller should be stopped before the deletion of CRD so that
		// it doesn't crash.
		r.engine.Stop(cclaim.ControllerName(xrd.GetName()))

		if err := r.local.Delete(ctx, local); rresource.IgnoreNotFound(err) != nil {
			return reconcile.Result{RequeueAfter: shortWait}, errors.Wrap(err, localPrefix+errDeleteCRD)
		}

		// We should be requeued implicitly because we're watching the
		// CustomResourceDefinition that we just deleted, but we requeue after
		// a tiny wait just in case the CRD isn't gone after the first requeue.
		xrd.Status.SetConditions(runtimev1alpha1.ReconcileSuccess())
		return reconcile.Result{RequeueAfter: tinyWait}, errors.Wrap(r.local.Status().Update(ctx, xrd), localPrefix+errUpdateStatus)
	}

	// After this point, we'll start operations that will need some cleanup
	// if XRD is deleted such as CRD creation and controller initialization. So,
	// we add a finalizer to make sure this Reconciler gets the chance to do
	// the cleanup before the XRD disappears from the api-server.
	if err := r.finalizer.AddFinalizer(ctx, xrd); err != nil {
		return reconcile.Result{RequeueAfter: shortWait}, errors.Wrap(err, localPrefix+errAddFinalizerXRD)
	}

	// We'll create or update the CRD of the claim type in local cluster to make
	// it available to users.
	meta.AddOwnerReference(local, meta.AsController(meta.ReferenceTo(xrd, v1alpha1.CompositeResourceDefinitionGroupVersionKind)))
	if err := r.local.Apply(ctx, local, rresource.MustBeControllableBy(xrd.GetUID())); err != nil {
		return reconcile.Result{RequeueAfter: shortWait}, errors.Wrap(err, localPrefix+errApplyCRD)
	}

	// It takes a little while for Kubernetes API Server to establish the new API
	// endpoints for the CRD. We'd like to make sure it's ready before starting
	// its controller.
	if !ccrd.IsEstablished(local.Status) {
		return reconcile.Result{RequeueAfter: tinyWait}, errors.Wrap(r.local.Status().Update(ctx, xrd), localPrefix+errUpdateStatus)
	}

	// The new controller for the type is configured with a reconciler and other
	// parameters that the reconciler requires.
	o := kcontroller.Options{Reconciler: claim.NewReconciler(r.mgr,
		r.remote,
		GroupVersionKindOf(*local),
		claim.WithLogger(log.WithValues("controller", cclaim.ControllerName(xrd.GetName()))),
		claim.WithRecorder(r.record.WithAnnotations("controller", cclaim.ControllerName(xrd.GetName()))),
	)}

	// Since we don't have strongly typed structs for the claims, we set the GVK
	// of Unstructured object so that controller-runtime is able to get events
	// of them via its unstructured client.
	rq := &kunstructured.Unstructured{}
	rq.SetGroupVersionKind(GroupVersionKindOf(*local))

	// We're all set for starting the controller. This assumes that ControllerEngine
	// Start call is idempotent, hence we don't check whether it was already started
	// or not.
	if err := r.engine.Start(cclaim.ControllerName(xrd.GetName()), o,
		controller.For(rq, &handler.EnqueueRequestForObject{}),
	); err != nil {
		return reconcile.Result{RequeueAfter: shortWait}, errors.Wrap(err, localPrefix+errStartController)
	}

	// The reconciliation is completed successfully.
	xrd.Status.SetConditions(runtimev1alpha1.ReconcileSuccess())
	return reconcile.Result{RequeueAfter: longWait}, errors.Wrap(r.local.Status().Update(ctx, xrd), localPrefix+errUpdateStatus)
}
