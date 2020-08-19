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
	timeout   = 2 * time.Minute
	longWait  = 1 * time.Minute
	shortWait = 30 * time.Second

	maxConcurrency = 5

	local       = "local cluster: "
	remote      = "remote cluster: "
	errGetCRD   = "cannot get custom resource definition"
	errApplyCRD = "cannot apply custom resource definition"
)

// NOTE(muvaf): CRDs could also be synced with apiextensions.Reconciler which syncs
// instances of InfraPubs/InfraDefs/Compositions. However, that controller first
// checks CRDs of those types and also assumes that it's the sole owner of that
// type and tries to make 1-to-1 match between remote and local cluster. In CRDs
// case, we are not the sole owner of all CRDs, i.e. we can't just delete a CRD
// in local cluster just because it doesn't exist in the remote cluster.
// The reconciler could be made configurable to handle both of these logics differently,
// but that's pretty much all the logic exists anyway. So, we keep the CRD reconciler
// separate and lean here.

// Setup adds a controller that watches CustomResourceDefinitions in the remote
// cluster and replicates them in the local cluster.
func Setup(mgr manager.Manager, localClient client.Client, logger logging.Logger) error {
	name := "CustomResourceDefinitions"
	ca := rresource.ClientApplicator{
		Client:     localClient,
		Applicator: rresource.NewAPIUpdatingApplicator(localClient),
	}
	r := NewReconciler(mgr, ca, logger)
	return ctrl.NewControllerManagedBy(mgr).
		Named(name).
		For(&v1beta1.CustomResourceDefinition{}).
		WithOptions(kcontroller.Options{MaxConcurrentReconciles: maxConcurrency}).
		WithEventFilter(resource.NewNameFilter([]types.NamespacedName{
			{Name: "infrastructuredefinitions.apiextensions.crossplane.io"},
			{Name: "infrastructurepublications.apiextensions.crossplane.io"},
			{Name: "compositions.apiextensions.crossplane.io"},
		})).
		Complete(r)
}

// NewReconciler returns a new *Reconciler.
func NewReconciler(mgr manager.Manager, localClientApplicator rresource.ClientApplicator, logger logging.Logger) *Reconciler {
	return &Reconciler{
		mgr:    mgr,
		local:  localClientApplicator,
		remote: mgr.GetClient(),
		log:    logger,
		// TODO(muvaf): The event recorders are constructed using manager but since
		// the manager is configured with the remote cluster, we are passing NopRecorder
		// for now until we figure out how we can construct an event recorder with
		// just kubeconfig.
		record: event.NewNopRecorder(),
	}
}

// Reconciler syncs CRDs in the remote cluster to the local cluster, overrides
// the existing ones in the local cluster. It's advised to use this together with
// an EventFilter to filter only the CRDs you'd like to be synced.
type Reconciler struct {
	mgr    ctrl.Manager
	local  rresource.ClientApplicator
	remote client.Client

	log    logging.Logger
	record event.Recorder
}

// Reconcile fetches the CRD from remote cluster and applies it in the local cluster.
func (r *Reconciler) Reconcile(req reconcile.Request) (reconcile.Result, error) {
	log := r.log.WithValues("request", req)
	log.Debug("Reconciling")

	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()

	crd := &v1beta1.CustomResourceDefinition{}
	if err := r.remote.Get(ctx, req.NamespacedName, crd); err != nil {
		return reconcile.Result{RequeueAfter: shortWait}, errors.Wrap(err, remote+errGetCRD)
	}
	// TODO(muvaf): Set condition on local CRD to tell when is the last time
	// it's been synced.
	return reconcile.Result{RequeueAfter: longWait}, errors.Wrap(r.local.Apply(ctx, crd, resource.OverrideGeneratedMetadata), local+errApplyCRD)
}
