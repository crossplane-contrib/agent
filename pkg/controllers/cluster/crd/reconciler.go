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
	"time"

	"github.com/pkg/errors"
	"k8s.io/apiextensions-apiserver/pkg/apis/apiextensions/v1beta1"
	"k8s.io/apimachinery/pkg/types"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	kcontroller "sigs.k8s.io/controller-runtime/pkg/controller"
	"sigs.k8s.io/controller-runtime/pkg/manager"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"

	"github.com/crossplane/crossplane-runtime/pkg/event"
	"github.com/crossplane/crossplane-runtime/pkg/logging"
	rresource "github.com/crossplane/crossplane-runtime/pkg/resource"

	"github.com/crossplane/agent/pkg/resource"
)

const (
	timeout        = 2 * time.Minute
	shortWait      = 30 * time.Second
	maxConcurrency = 5
)

func Setup(mgr manager.Manager, localClient client.Client, logger logging.Logger) error {
	name := "CustomResourceDefinitions"
	r := &Reconciler{
		mgr: mgr,
		local: rresource.ClientApplicator{
			Client:     localClient,
			Applicator: rresource.NewAPIUpdatingApplicator(localClient),
		},
		remote: mgr.GetClient(),
		log:    logger,
		// TODO(muvaf): The event recorders are constructed using manager but since
		// the manager is configured with the remote cluster, we are passing NopRecorder
		// for now until we figure out how we can construct an event recorder with
		// just kubeconfig.
		record: event.NewNopRecorder(),
	}
	return ctrl.NewControllerManagedBy(mgr).
		Named(name).
		For(&v1beta1.CustomResourceDefinition{}).
		WithOptions(kcontroller.Options{MaxConcurrentReconciles: maxConcurrency}).
		WithEventFilter(NewNameFilter([]types.NamespacedName{
			{Name: "infrastructuredefinitions.apiextensions.crossplane.io"},
			{Name: "infrastructurepublications.apiextensions.crossplane.io"},
			{Name: "compositions.apiextensions.crossplane.io"},
		})).
		Complete(r)
}

type Reconciler struct {
	mgr    ctrl.Manager
	local  rresource.ClientApplicator
	remote client.Client

	list []string

	log    logging.Logger
	record event.Recorder
}

func (r *Reconciler) Reconcile(req reconcile.Request) (reconcile.Result, error) {
	log := r.log.WithValues("request", req)
	log.Debug("Reconciling")

	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()

	crd := &v1beta1.CustomResourceDefinition{}
	if err := r.remote.Get(ctx, req.NamespacedName, crd); err != nil {
		return reconcile.Result{RequeueAfter: shortWait}, errors.Wrap(err, "cannot get crd in remote cluster")
	}
	existing := &v1beta1.CustomResourceDefinition{}
	if err := r.local.Get(ctx, req.NamespacedName, crd); rresource.IgnoreNotFound(err) != nil {
		return reconcile.Result{RequeueAfter: shortWait}, errors.Wrap(err, "cannot get crd in local cluster")
	}
	resource.EqualizeMetadata(existing, crd)
	return reconcile.Result{RequeueAfter: shortWait}, errors.Wrap(r.local.Apply(ctx, crd), "cannot apply in local cluster")
}
