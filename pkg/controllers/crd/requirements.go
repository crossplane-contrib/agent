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

package crd

import (
	"context"
	"fmt"

	"github.com/pkg/errors"
	"k8s.io/apiextensions-apiserver/pkg/apis/apiextensions/v1beta1"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/types"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	kcontroller "sigs.k8s.io/controller-runtime/pkg/controller"
	"sigs.k8s.io/controller-runtime/pkg/manager"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"

	"github.com/crossplane/crossplane-runtime/pkg/event"
	"github.com/crossplane/crossplane-runtime/pkg/logging"
	"github.com/crossplane/crossplane-runtime/pkg/meta"
	rresource "github.com/crossplane/crossplane-runtime/pkg/resource"
	"github.com/crossplane/crossplane/apis/apiextensions/v1alpha1"
	"github.com/crossplane/crossplane/apis/apiextensions/v1alpha1/ccrd"

	"github.com/crossplane/agent/pkg/resource"
)

func SetupRequirementCRD(mgr manager.Manager, remoteClient client.Client, logger logging.Logger) error {
	name := "RequirementCRD"
	r := &RequirementCRDReconciler{
		mgr: mgr,
		local: rresource.ClientApplicator{
			Client:     mgr.GetClient(),
			Applicator: rresource.NewAPIUpdatingApplicator(mgr.GetClient()),
		},
		remote: remoteClient,
		log:    logger,
		record: event.NewAPIRecorder(mgr.GetEventRecorderFor(name)),
	}
	return ctrl.NewControllerManagedBy(mgr).
		Named(name).
		For(&v1alpha1.InfrastructurePublication{}).
		WithOptions(kcontroller.Options{MaxConcurrentReconciles: maxConcurrency}).
		Complete(r)
}

type RequirementCRDReconciler struct {
	mgr    ctrl.Manager
	local  rresource.ClientApplicator
	remote client.Client

	log    logging.Logger
	record event.Recorder
}

func (r *RequirementCRDReconciler) Reconcile(req reconcile.Request) (reconcile.Result, error) {
	log := r.log.WithValues("request", req)
	log.Debug("Reconciling")

	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()

	p := &v1alpha1.InfrastructurePublication{}
	if err := r.local.Get(ctx, req.NamespacedName, p); err != nil {
		return reconcile.Result{RequeueAfter: shortWait}, errors.Wrap(err, "cannot get infrastructurepublication in the local cluster")
	}
	gk := schema.ParseGroupKind(p.Spec.InfrastructureDefinitionReference.Name)
	crd := &v1beta1.CustomResourceDefinition{}
	nn := types.NamespacedName{Name: fmt.Sprintf("%s%s.%s", gk.Kind[:len(gk.Kind)-1], ccrd.PublishedInfrastructureSuffixPlural, gk.Group)}
	if err := r.remote.Get(ctx, nn, crd); err != nil {
		return reconcile.Result{RequeueAfter: shortWait}, errors.Wrap(err, "cannot get CRD in the local cluster")
	}
	existing := &v1beta1.CustomResourceDefinition{}
	if err := r.local.Get(ctx, nn, existing); rresource.IgnoreNotFound(err) != nil {
		return reconcile.Result{RequeueAfter: shortWait}, errors.Wrap(err, "cannot get CRD in the local cluster")
	}
	resource.EqualizeMetadata(existing, crd)
	meta.AddOwnerReference(crd, meta.AsController(meta.ReferenceTo(p, v1alpha1.InfrastructurePublicationGroupVersionKind)))
	return reconcile.Result{RequeueAfter: longWait}, errors.Wrap(r.local.Apply(ctx, crd), "cannot apply CRD in the local cluster")
}
