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
	"time"

	"k8s.io/apimachinery/pkg/runtime/schema"

	"github.com/crossplane/crossplane-runtime/pkg/resource"

	"github.com/crossplane/crossplane/apis/apiextensions/v1alpha1/ccrd"
	"k8s.io/apiextensions-apiserver/pkg/apis/apiextensions/v1beta1"
	"k8s.io/apimachinery/pkg/types"

	v1 "k8s.io/api/core/v1"

	"github.com/pkg/errors"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	kcontroller "sigs.k8s.io/controller-runtime/pkg/controller"
	"sigs.k8s.io/controller-runtime/pkg/manager"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"

	"github.com/crossplane/crossplane-runtime/pkg/event"
	"github.com/crossplane/crossplane-runtime/pkg/logging"
	"github.com/crossplane/crossplane/apis/apiextensions/v1alpha1"
)

const (
	maxConcurrency = 5
	timeout        = 2 * time.Minute
	shortWait      = 30 * time.Second
	longWait       = 1 * time.Minute
)

// Setup adds a controller that reconciles ApplicationConfigurations.
func Setup(mgr ctrl.Manager, localClient client.Client, log logging.Logger) error {
	name := "InfrastructurePublications"
	r := NewReconciler(mgr,
		WithLogger(log.WithValues("controller", name)),
		WithRecorder(event.NewAPIRecorder(mgr.GetEventRecorderFor(name))),
		WithLocalClient(resource.ClientApplicator{
			Client:     localClient,
			Applicator: resource.NewAPIUpdatingApplicator(localClient),
		}))

	return ctrl.NewControllerManagedBy(mgr).
		Named(name).
		For(&v1alpha1.InfrastructurePublication{}).
		WithOptions(kcontroller.Options{MaxConcurrentReconciles: maxConcurrency}).
		Complete(r)
}

// ReconcilerOption is used to configure the Reconciler.
type ReconcilerOption func(*Reconciler)

// WithLocalClient specifies the Client of the local cluster that Reconciler
// should create resources in.
func WithLocalClient(cl resource.ClientApplicator) ReconcilerOption {
	return func(r *Reconciler) {
		r.local = cl
	}
}

// WithRemoteClient specifies the Client of the remote cluster that Reconciler
// should read resources from. Defaults to the manager's client.
func WithRemoteClient(cl client.Client) ReconcilerOption {
	return func(r *Reconciler) {
		r.remote = cl
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

func NewReconciler(mgr manager.Manager, opts ...ReconcilerOption) *Reconciler {
	r := &Reconciler{
		mgr:    mgr,
		log:    logging.NewNopLogger(),
		remote: mgr.GetClient(),
	}

	for _, f := range opts {
		f(r)
	}

	return r
}

type Reconciler struct {
	remote client.Client
	local  resource.ClientApplicator
	mgr    manager.Manager

	log    logging.Logger
	record event.Recorder
}

func (r *Reconciler) Reconcile(req reconcile.Request) (reconcile.Result, error) {
	log := r.log.WithValues("request", req)
	log.Debug("Reconciling")

	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()

	p := &v1alpha1.InfrastructurePublication{}
	if err := r.remote.Get(ctx, req.NamespacedName, p); err != nil {
		return reconcile.Result{RequeueAfter: shortWait}, errors.Wrap(err, "cannot get infrastructurepublication to reconcile")
	}
	if p.Status.GetCondition(v1alpha1.TypeEstablished).Status != v1.ConditionTrue {
		return reconcile.Result{RequeueAfter: shortWait}, errors.New("infrastructurepublication in remote cluster is not established yet")
	}
	gk := schema.ParseGroupKind(p.Spec.InfrastructureDefinitionReference.Name)
	crd := &v1beta1.CustomResourceDefinition{}
	nn := types.NamespacedName{Name: fmt.Sprintf("%s%s.%s", gk.Kind[:len(gk.Kind)-1], ccrd.PublishedInfrastructureSuffixPlural, gk.Group)}
	if err := r.remote.Get(ctx, nn, crd); err != nil {
		return reconcile.Result{RequeueAfter: shortWait}, errors.Wrap(err, "cannot get CRD from remote cluster")
	}
	if !ccrd.IsEstablished(crd.Status) {
		return reconcile.Result{RequeueAfter: shortWait}, errors.New("crd in remote cluster is not established yet")
	}
	existing := &v1beta1.CustomResourceDefinition{}
	if err := r.local.Get(ctx, nn, existing); resource.IgnoreNotFound(err) != nil {
		return reconcile.Result{RequeueAfter: shortWait}, errors.Wrap(err, "cannot get crd in local cluster")
	}
	EqualizeMetadata(existing, crd)
	if err := r.local.Apply(ctx, crd); err != nil {
		return reconcile.Result{RequeueAfter: shortWait}, errors.Wrap(err, "cannot update crd in local cluster")
	}
	return reconcile.Result{RequeueAfter: longWait}, nil
}
