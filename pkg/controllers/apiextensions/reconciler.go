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

package apiextensions

import (
	"context"
	"fmt"
	"time"

	"github.com/pkg/errors"
	"k8s.io/apiextensions-apiserver/pkg/apis/apiextensions/v1beta1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/manager"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"

	"github.com/crossplane/crossplane-runtime/pkg/event"
	"github.com/crossplane/crossplane-runtime/pkg/logging"
	rresource "github.com/crossplane/crossplane-runtime/pkg/resource"
	"github.com/crossplane/crossplane/apis/apiextensions/v1alpha1/ccrd"

	"github.com/crossplane/agent/pkg/resource"
)

const (
	timeout   = 2 * time.Minute
	longWait  = 1 * time.Minute
	shortWait = 30 * time.Second
	tinyWait  = 3 * time.Second

	localPrefix          = "local cluster: "
	remotePrefix         = "remote cluster: "
	errGetCRD            = "cannot get custom resource definition"
	errGetInstanceFmt    = "cannot get %s instance"
	errListInstanceFmt   = "cannot list %s instances"
	errDeleteInstanceFmt = "cannot delete %s instance"
	errApplyInstanceFmt  = "cannot apply %s instance"
)

// ReconcilerOption is used to configure the Reconciler.
type ReconcilerOption func(*Reconciler)

// WithNewObjectListFn specifies the function to be used to initialize an empty
// list of objects whose type is being reconciled by this Reconciler.
func WithNewObjectListFn(f func() runtime.Object) ReconcilerOption {
	return func(r *Reconciler) {
		r.newObjectList = f
	}
}

// WithNewInstanceFn specifies the function to be used to initialize an empty
// object whose type is being reconciled by this Reconciler.
func WithNewInstanceFn(f func() rresource.Object) ReconcilerOption {
	return func(r *Reconciler) {
		r.newObject = f
	}
}

// WithGetItemsFn specifies the function that will be used to retrieve an array
// of objects from the object list.
func WithGetItemsFn(f func(l runtime.Object) []rresource.Object) ReconcilerOption {
	return func(r *Reconciler) {
		r.getItems = f
	}
}

// WithCRDName specifies the name of the corresponding CRD object that has to be
// available in the local cluster.
func WithCRDName(name string) ReconcilerOption {
	return func(r *Reconciler) {
		r.crdName = types.NamespacedName{Name: name}
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

// NewReconciler returns a new *Reconciler object.
func NewReconciler(mgr manager.Manager, localClientApplicator rresource.ClientApplicator, opts ...ReconcilerOption) *Reconciler {
	r := &Reconciler{
		mgr:    mgr,
		log:    logging.NewNopLogger(),
		remote: mgr.GetClient(),
		local:  localClientApplicator,
	}

	for _, f := range opts {
		f(r)
	}

	return r
}

// Reconciler syncs the Custom Resources of given CustomResourceDefinition from
// remote cluster to local cluster. It works only with cluster-scoped resources and
// always overrides the changes made to those Custom Resources in the local cluster.
type Reconciler struct {
	remote client.Client
	local  rresource.ClientApplicator
	mgr    manager.Manager

	crdName       types.NamespacedName
	newObjectList func() runtime.Object
	getItems      func(l runtime.Object) []rresource.Object
	newObject     func() rresource.Object

	log    logging.Logger
	record event.Recorder
}

// Reconcile syncs the cluster-scoped instance of the type in remote->local direction.
func (r *Reconciler) Reconcile(req reconcile.Request) (reconcile.Result, error) {
	log := r.log.WithValues("request", req)
	log.Debug("Reconciling")

	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()

	crd := &v1beta1.CustomResourceDefinition{}
	if err := r.local.Get(ctx, r.crdName, crd); err != nil {
		return reconcile.Result{RequeueAfter: shortWait}, errors.Wrap(err, localPrefix+errGetCRD)
	}
	if !ccrd.IsEstablished(crd.Status) {
		return reconcile.Result{RequeueAfter: tinyWait}, nil
	}

	ro := r.newObject()
	if err := r.remote.Get(ctx, req.NamespacedName, ro); err != nil {
		return reconcile.Result{RequeueAfter: shortWait}, errors.Wrap(err, remotePrefix+fmt.Sprintf(errGetInstanceFmt, r.crdName.Name))
	}
	lo := resource.SanitizedDeepCopyObject(ro)
	if err := r.local.Apply(ctx, lo); err != nil {
		return reconcile.Result{RequeueAfter: shortWait}, errors.Wrap(err, localPrefix+fmt.Sprintf(errApplyInstanceFmt, r.crdName.Name))
	}
	// TODO(muvaf): We need to call status update to bring the status subresource
	// of the resources.

	// When an instance in the remote cluster is deleted, it's not guaranteed that
	// we will get a deletion event for a number of reasons including agent not
	// being up at that time. Since reconciliation is called only for the existing
	// resources, we need to delete the resources in the local that do not have
	// a corresponding resource in the remote cluster.
	removalList := map[string]bool{}
	ll := r.newObjectList()
	if err := r.local.List(ctx, ll); err != nil {
		return reconcile.Result{RequeueAfter: shortWait}, errors.Wrap(err, localPrefix+fmt.Sprintf(errListInstanceFmt, r.crdName.Name))
	}
	for _, obj := range r.getItems(ll) {
		removalList[obj.GetName()] = true
	}
	rl := r.newObjectList()
	if err := r.remote.List(ctx, rl); err != nil {
		return reconcile.Result{RequeueAfter: shortWait}, errors.Wrap(err, remotePrefix+fmt.Sprintf(errListInstanceFmt, r.crdName.Name))
	}
	for _, obj := range r.getItems(rl) {
		delete(removalList, obj.GetName())
	}
	for remove := range removalList {
		obj := r.newObject()
		obj.SetName(remove)
		if err := r.local.Delete(ctx, obj); rresource.IgnoreNotFound(err) != nil {
			return reconcile.Result{RequeueAfter: shortWait}, errors.Wrap(err, localPrefix+fmt.Sprintf(errDeleteInstanceFmt, r.crdName.Name))
		}
	}
	return reconcile.Result{RequeueAfter: longWait}, nil
}
