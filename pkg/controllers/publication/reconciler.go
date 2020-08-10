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
	"fmt"
	"time"

	"github.com/crossplane/agent/pkg/controllers/requirement"

	runtimev1alpha1 "github.com/crossplane/crossplane-runtime/apis/core/v1alpha1"
	"github.com/pkg/errors"
	"k8s.io/apiextensions-apiserver/pkg/apis/apiextensions/v1beta1"
	kmeta "k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	kunstructured "k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/types"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	kcontroller "sigs.k8s.io/controller-runtime/pkg/controller"
	"sigs.k8s.io/controller-runtime/pkg/handler"
	"sigs.k8s.io/controller-runtime/pkg/manager"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"

	"github.com/crossplane/crossplane-runtime/pkg/controller"
	"github.com/crossplane/crossplane-runtime/pkg/event"
	"github.com/crossplane/crossplane-runtime/pkg/logging"
	"github.com/crossplane/crossplane-runtime/pkg/meta"
	rresource "github.com/crossplane/crossplane-runtime/pkg/resource"
	"github.com/crossplane/crossplane/apis/apiextensions/v1alpha1"
	"github.com/crossplane/crossplane/apis/apiextensions/v1alpha1/ccrd"
	crequirement "github.com/crossplane/crossplane/pkg/controller/apiextensions/requirement"

	"github.com/crossplane/agent/pkg/resource"
)

const (
	timeout   = 2 * time.Minute
	longWait  = 1 * time.Minute
	shortWait = 30 * time.Second
	tinyWait  = 3 * time.Second

	finalizer = "agent.crossplane.io/requirement-crd-controller"

	local             = "local cluster: "
	remote            = "remote cluster: "
	errUpdateStatus   = "cannot update status of infrastructure publication"
	errGetPublication = "cannot get infrastructure publication"
	errGetCRD         = "cannot get custom resoruce definition"
	errApplyCRD       = "cannot apply custom resource definition"
)

func Setup(mgr manager.Manager, remoteClient client.Client, logger logging.Logger) error {
	name := "RequirementCRD"
	r := &Reconciler{
		mgr: mgr,
		local: rresource.ClientApplicator{
			Client:     mgr.GetClient(),
			Applicator: rresource.NewAPIUpdatingApplicator(mgr.GetClient()),
		},
		remote:    remoteClient,
		engine:    controller.NewEngine(mgr),
		finalizer: rresource.NewAPIFinalizer(mgr.GetClient(), finalizer),
		log:       logger,
		record:    event.NewAPIRecorder(mgr.GetEventRecorderFor(name)),
	}
	return ctrl.NewControllerManagedBy(mgr).
		Named(name).
		For(&v1alpha1.InfrastructurePublication{}).
		Owns(&v1beta1.CustomResourceDefinition{}).
		Complete(r)
}

type Reconciler struct {
	mgr    ctrl.Manager
	local  rresource.ClientApplicator
	remote client.Client

	engine    *controller.Engine
	finalizer rresource.Finalizer

	log    logging.Logger
	record event.Recorder
}

func (r *Reconciler) Reconcile(req reconcile.Request) (reconcile.Result, error) {
	log := r.log.WithValues("request", req)
	log.Debug("Reconciling")

	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()

	p := &v1alpha1.InfrastructurePublication{}
	if err := r.local.Get(ctx, req.NamespacedName, p); err != nil {
		return reconcile.Result{RequeueAfter: shortWait}, errors.Wrap(err, local+errGetPublication)
	}
	gk := schema.ParseGroupKind(p.Spec.InfrastructureDefinitionReference.Name)
	crd := &v1beta1.CustomResourceDefinition{}
	nn := types.NamespacedName{Name: fmt.Sprintf("%s%s.%s", gk.Kind[:len(gk.Kind)-1], ccrd.PublishedInfrastructureSuffixPlural, gk.Group)}
	if err := r.remote.Get(ctx, nn, crd); err != nil {
		return reconcile.Result{RequeueAfter: shortWait}, errors.Wrap(err, remote+errGetCRD)
	}
	existing := &v1beta1.CustomResourceDefinition{}
	if err := r.local.Get(ctx, nn, existing); rresource.IgnoreNotFound(err) != nil {
		return reconcile.Result{RequeueAfter: shortWait}, errors.Wrap(err, local+errGetCRD)
	}
	gvk := schema.GroupVersionKind{
		Group:   crd.Spec.Group,
		Kind:    crd.Spec.Names.Kind,
		Version: crd.Spec.Versions[len(crd.Spec.Versions)-1].Name,
	}
	if meta.WasDeleted(p) {
		p.Status.SetConditions(v1alpha1.Deleting())

		nn := types.NamespacedName{Name: crd.GetName()}
		if err := r.local.Get(ctx, nn, crd); rresource.IgnoreNotFound(err) != nil {
			// TODO(muvaf): Error messages could be more helpful if we automate
			// injection of information about which cluster resource exists.
			return reconcile.Result{RequeueAfter: shortWait}, errors.Wrap(r.local.Status().Update(ctx, p), errUpdateStatus)
		}

		// The CRD has no creation timestamp, or we don't control it. Most
		// likely we successfully deleted it on a previous reconcile. It's also
		// possible that we're being asked to delete it before we got around to
		// creating it, or that we lost control of it around the same time we
		// were deleted. In the (presumably exceedingly rare) latter case we'll
		// orphan the CRD.
		if !meta.WasCreated(crd) || !metav1.IsControlledBy(crd, p) {
			// It's likely that we've already stopped this controller on a
			// previous reconcile, but we try again just in case. This is a
			// no-op if the controller was already stopped.
			r.engine.Stop(crequirement.ControllerName(p.GetName()))

			if err := r.finalizer.RemoveFinalizer(ctx, crd); err != nil {
				return reconcile.Result{RequeueAfter: shortWait}, errors.Wrap(r.local.Status().Update(ctx, p), local+errUpdateStatus)
			}

			// We're all done deleting and have removed our finalizer. There's
			// no need to requeue because there's nothing left to do.
			return reconcile.Result{Requeue: false}, nil
		}

		l := &kunstructured.UnstructuredList{}
		l.SetGroupVersionKind(gvk)
		if err := r.local.List(ctx, l); rresource.Ignore(kmeta.IsNoMatchError, err) != nil {
			return reconcile.Result{RequeueAfter: shortWait}, errors.Wrap(r.local.Status().Update(ctx, p), local+errUpdateStatus)
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
					return reconcile.Result{RequeueAfter: shortWait}, errors.Wrap(r.local.Status().Update(ctx, p), local+errUpdateStatus)
				}
			}

			// We requeue to confirm that all the custom resources we just
			// deleted are actually gone. We need to requeue after a tiny wait
			// because we won't be requeued implicitly when the CRs are deleted.
			return reconcile.Result{RequeueAfter: tinyWait}, errors.Wrap(r.local.Status().Update(ctx, p), local+errUpdateStatus)
		}

		// The controller should be stopped before the deletion of CRD so that
		// it doesn't crash.
		r.engine.Stop(crequirement.ControllerName(p.GetName()))

		if err := r.local.Delete(ctx, crd); rresource.IgnoreNotFound(err) != nil {
			return reconcile.Result{RequeueAfter: shortWait}, errors.Wrap(r.local.Status().Update(ctx, p), local+errUpdateStatus)
		}

		// We should be requeued implicitly because we're watching the
		// CustomResourceDefinition that we just deleted, but we requeue after
		// a tiny wait just in case the CRD isn't gone after the first requeue.
		p.Status.SetConditions(runtimev1alpha1.ReconcileSuccess())
		return reconcile.Result{RequeueAfter: tinyWait}, errors.Wrap(r.local.Status().Update(ctx, p), local+errUpdateStatus)
	}

	resource.OverrideOutputMetadata(existing, crd)
	meta.AddOwnerReference(crd, meta.AsController(meta.ReferenceTo(p, v1alpha1.InfrastructurePublicationGroupVersionKind)))
	if err := r.local.Apply(ctx, crd); err != nil {
		return reconcile.Result{RequeueAfter: shortWait}, errors.Wrap(err, local+errApplyCRD)
	}

	if !ccrd.IsEstablished(crd.Status) {
		return reconcile.Result{RequeueAfter: tinyWait}, errors.Wrap(r.local.Status().Update(ctx, p), local+errUpdateStatus)
	}
	o := kcontroller.Options{Reconciler: requirement.NewReconciler(r.mgr,
		r.remote,
		gvk,
		requirement.WithLogger(log.WithValues("controller", crequirement.ControllerName(p.GetName()))),
		requirement.WithRecorder(r.record.WithAnnotations("controller", crequirement.ControllerName(p.GetName()))),
	)}

	rq := &kunstructured.Unstructured{}
	rq.SetGroupVersionKind(gvk)

	if err := r.engine.Start(crequirement.ControllerName(p.GetName()), o,
		controller.For(rq, &handler.EnqueueRequestForObject{}),
	); err != nil {
		return reconcile.Result{RequeueAfter: shortWait}, errors.Wrap(r.local.Status().Update(ctx, p), local+errUpdateStatus)
	}

	p.Status.SetConditions(v1alpha1.Started())
	p.Status.SetConditions(runtimev1alpha1.ReconcileSuccess())
	return reconcile.Result{RequeueAfter: longWait}, errors.Wrap(r.local.Status().Update(ctx, p), local+errUpdateStatus)
}
