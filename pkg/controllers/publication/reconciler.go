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

package publication

import (
	"context"

	"time"

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
	crequirement "github.com/crossplane/crossplane/pkg/controller/apiextensions/requirement"

	"github.com/crossplane/agent/pkg/controllers/requirement"
)

const (
	timeout   = 2 * time.Minute
	longWait  = 1 * time.Minute
	shortWait = 30 * time.Second
	tinyWait  = 3 * time.Second

	finalizer = "agent.crossplane.io/requirement-crd-controller"

	localPrefix        = "local cluster: "
	remotePrefix       = "remote cluster: "
	errUpdateStatus    = "cannot update status of infrastructure publication"
	errStartController = "cannot start controller"
	errRemoveFinalizer = "cannot remove finalizer"
	errGetPublication  = "cannot get infrastructure publication"
	errRenderCRD       = "cannot render a crd from remote infrastructurepublication"
	errGetCRD          = "cannot get custom resoruce definition"
	errApplyCRD        = "cannot apply custom resource definition"
	errListCR          = "cannot list custom resources of published type"
	errDeleteCR        = "cannot delete custom resources of published type"
	errDeleteCRD       = "cannot delete crd of published type"
	errAddFinalizerPub = "cannot add finalizer to infrastructurepublication"
)

// Setup adds a controller that will reconcile InfrastructurePublications in the
// local cluster and create CRDs & controllers that will reconcile those new types.
func Setup(mgr manager.Manager, remoteClient client.Client, logger logging.Logger) error {
	name := "RequirementCRD"
	r := NewReconciler(mgr, remoteClient,
		WithCRDRenderer(NewAPIRemoteCRDRenderer(remoteClient)),
		WithLogger(logger),
		WithRecorder(event.NewAPIRecorder(mgr.GetEventRecorderFor(name))))
	return ctrl.NewControllerManagedBy(mgr).
		Named(name).
		For(&v1alpha1.InfrastructurePublication{}).
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

// WithCRDRenderer specifies how the Reconciler should render CRDs.
func WithCRDRenderer(re CRDRenderer) ReconcilerOption {
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
		crd:       NewNopRenderer(),
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

// CRDRenderer can be satisfied with objects that can return a CRD with InfrastructurePublication
// information.
type CRDRenderer interface {
	Render(ctx context.Context, ip v1alpha1.InfrastructurePublication) (*v1beta1.CustomResourceDefinition, error)
}

// Reconciler watches the InfrastructurePublications in the cluster and creates a
// CRD for each of them with spec that is rendered via supplied CRDRenderer. Then
// it creates a controller for each new type that will sync the instances of that
// type from local cluster to remote cluster.
type Reconciler struct {
	mgr    ctrl.Manager
	local  rresource.ClientApplicator
	remote client.Client

	crd       CRDRenderer
	engine    ControllerEngine
	finalizer rresource.Finalizer

	log    logging.Logger
	record event.Recorder
}

// Reconcile reconciles InfrastructurePublication and does the necessary operations
// to bootstrap reconciliation of that new type defined by InfrastructurePublication.
func (r *Reconciler) Reconcile(req reconcile.Request) (reconcile.Result, error) {
	log := r.log.WithValues("request", req)
	log.Debug("Reconciling")

	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()

	p := &v1alpha1.InfrastructurePublication{}
	if err := r.local.Get(ctx, req.NamespacedName, p); rresource.IgnoreNotFound(err) != nil {
		return reconcile.Result{RequeueAfter: shortWait}, errors.Wrap(err, localPrefix+errGetPublication)
	}
	local, err := r.crd.Render(ctx, *p)
	if rresource.IgnoreNotFound(err) != nil {
		return reconcile.Result{RequeueAfter: shortWait}, errors.Wrap(err, remotePrefix+errRenderCRD)
	}

	if meta.WasDeleted(p) {
		p.Status.SetConditions(v1alpha1.Deleting())

		err := r.local.Get(ctx, CRDNameOf(*p), local)
		if rresource.IgnoreNotFound(err) != nil {
			return reconcile.Result{RequeueAfter: shortWait}, errors.Wrap(err, localPrefix+errGetCRD)
		}

		// The CRD has no creation timestamp, or we don't control it. Most
		// likely we successfully deleted it on a previous reconcile. It's also
		// possible that we're being asked to delete it before we got around to
		// creating it, or that we lost control of it around the same time we
		// were deleted. In the (presumably exceedingly rare) latter case we'll
		// orphan the CRD.
		if !meta.WasCreated(local) || !metav1.IsControlledBy(local, p) {
			// It's likely that we've already stopped this controller on a
			// previous reconcile, but we try again just in case. This is a
			// no-op if the controller was already stopped.
			r.engine.Stop(crequirement.ControllerName(p.GetName()))

			if err := r.finalizer.RemoveFinalizer(ctx, p); err != nil {
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
			// TODO(negz): DeleteAllOf does not work here, despite working in
			// the definition controller. Could this be due to requirements
			// being namespaced rather than cluster scoped?
			for i := range l.Items {
				if err := r.local.Delete(ctx, &l.Items[i]); rresource.IgnoreNotFound(err) != nil {
					return reconcile.Result{RequeueAfter: shortWait}, errors.Wrap(err, localPrefix+errDeleteCR)
				}
			}

			// We requeue to confirm that all the custom resources we just
			// deleted are actually gone. We need to requeue after a tiny wait
			// because we won't be requeued implicitly when the CRs are deleted.
			return reconcile.Result{RequeueAfter: tinyWait}, errors.Wrap(r.local.Status().Update(ctx, p), localPrefix+errUpdateStatus)
		}

		// The controller should be stopped before the deletion of CRD so that
		// it doesn't crash.
		r.engine.Stop(crequirement.ControllerName(p.GetName()))

		if err := r.local.Delete(ctx, local); rresource.IgnoreNotFound(err) != nil {
			return reconcile.Result{RequeueAfter: shortWait}, errors.Wrap(err, localPrefix+errDeleteCRD)
		}

		// We should be requeued implicitly because we're watching the
		// CustomResourceDefinition that we just deleted, but we requeue after
		// a tiny wait just in case the CRD isn't gone after the first requeue.
		p.Status.SetConditions(runtimev1alpha1.ReconcileSuccess())
		return reconcile.Result{RequeueAfter: tinyWait}, errors.Wrap(r.local.Status().Update(ctx, p), localPrefix+errUpdateStatus)
	}

	if err := r.finalizer.AddFinalizer(ctx, p); err != nil {
		return reconcile.Result{RequeueAfter: shortWait}, errors.Wrap(err, localPrefix+errAddFinalizerPub)
	}

	meta.AddOwnerReference(local, meta.AsController(meta.ReferenceTo(p, v1alpha1.InfrastructurePublicationGroupVersionKind)))
	if err := r.local.Apply(ctx, local, rresource.MustBeControllableBy(p.GetUID())); err != nil {
		return reconcile.Result{RequeueAfter: shortWait}, errors.Wrap(err, localPrefix+errApplyCRD)
	}

	if !ccrd.IsEstablished(local.Status) {
		return reconcile.Result{RequeueAfter: tinyWait}, errors.Wrap(r.local.Status().Update(ctx, p), localPrefix+errUpdateStatus)
	}
	o := kcontroller.Options{Reconciler: requirement.NewReconciler(r.mgr,
		r.remote,
		GroupVersionKindOf(*local),
		requirement.WithLogger(log.WithValues("controller", crequirement.ControllerName(p.GetName()))),
		requirement.WithRecorder(r.record.WithAnnotations("controller", crequirement.ControllerName(p.GetName()))),
	)}

	rq := &kunstructured.Unstructured{}
	rq.SetGroupVersionKind(GroupVersionKindOf(*local))

	if err := r.engine.Start(crequirement.ControllerName(p.GetName()), o,
		controller.For(rq, &handler.EnqueueRequestForObject{}),
	); err != nil {
		return reconcile.Result{RequeueAfter: shortWait}, errors.Wrap(err, localPrefix+errStartController)
	}

	p.Status.SetConditions(v1alpha1.Started())
	p.Status.SetConditions(runtimev1alpha1.ReconcileSuccess())
	return reconcile.Result{RequeueAfter: longWait}, errors.Wrap(r.local.Status().Update(ctx, p), localPrefix+errUpdateStatus)
}
