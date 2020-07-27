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
	shortWait = 30 * time.Second
	longWait  = 1 * time.Minute
)

// ReconcilerOption is used to configure the Reconciler.
type ReconcilerOption func(*Reconciler)

func WithNewListFn(f func() runtime.Object) ReconcilerOption {
	return func(r *Reconciler) {
		r.newInstanceList = f
	}
}

func WithNewInstanceFn(f func() rresource.Object) ReconcilerOption {
	return func(r *Reconciler) {
		r.newInstance = f
	}
}

func WithGetItemsFn(f func(l runtime.Object) []rresource.Object) ReconcilerOption {
	return func(r *Reconciler) {
		r.getItems = f
	}
}

// WithCRDName specifies the name of the corresponding CRD that has to be made
// available in the local cluster.
func WithCRDName(name string) ReconcilerOption {
	return func(r *Reconciler) {
		r.crdName = types.NamespacedName{Name: name}
	}
}

// WithLocalClient specifies the Client of the local cluster that Reconciler
// should create resources in.
func WithLocalClient(cl rresource.ClientApplicator) ReconcilerOption {
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
	local  rresource.ClientApplicator
	mgr    manager.Manager

	crdName         types.NamespacedName
	newInstanceList func() runtime.Object
	getItems        func(l runtime.Object) []rresource.Object
	newInstance     func() rresource.Object

	log    logging.Logger
	record event.Recorder
}

func (r *Reconciler) Reconcile(req reconcile.Request) (reconcile.Result, error) {
	log := r.log.WithValues("request", req)
	log.Debug("Reconciling")

	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()

	crd := &v1beta1.CustomResourceDefinition{}
	if err := r.local.Get(ctx, r.crdName, crd); err != nil {
		return reconcile.Result{RequeueAfter: shortWait}, errors.Wrap(err, "cannot get crds in the local cluster")
	}
	if !ccrd.IsEstablished(crd.Status) {
		return reconcile.Result{RequeueAfter: shortWait}, errors.New("crd in local cluster is not established yet")
	}

	instance := r.newInstance()
	if err := r.remote.Get(ctx, req.NamespacedName, instance); err != nil {
		return reconcile.Result{RequeueAfter: shortWait}, errors.Wrap(err, "cannot get instance in the remote cluster")
	}
	existing := r.newInstance()
	// TODO(muvaf): This Get-Equalize-Apply is used in almost all reconcilers.
	// We should implement an `Applicator` to do this automatically.
	if err := r.local.Get(ctx, req.NamespacedName, existing); rresource.IgnoreNotFound(err) != nil {
		return reconcile.Result{RequeueAfter: shortWait}, errors.Wrap(err, "cannot get instance in the local cluster")
	}

	// TODO(muvaf): We need to call status update to bring the status subresource.
	resource.OverrideOutputMetadata(existing, instance)
	if err := r.local.Apply(ctx, instance); err != nil {
		return reconcile.Result{RequeueAfter: shortWait}, errors.Wrap(err, "cannot apply instance in the local cluster")
	}
	return reconcile.Result{RequeueAfter: longWait}, errors.Wrap(r.Cleanup(ctx), "cannot clean up the local cluster")
}

func (r *Reconciler) Cleanup(ctx context.Context) error {
	removalList := map[string]bool{}
	ll := r.newInstanceList()
	if err := r.local.List(ctx, ll); err != nil {
		return errors.Wrap(err, "cannot list instances in the local cluster")
	}
	for _, obj := range r.getItems(ll) {
		removalList[obj.GetName()] = true
	}
	rl := r.newInstanceList()
	if err := r.remote.List(ctx, rl); err != nil {
		return errors.Wrap(err, "cannot list instances in the remote cluster")
	}
	for _, obj := range r.getItems(rl) {
		delete(removalList, obj.GetName())
	}
	for remove := range removalList {
		obj := r.newInstance()
		obj.SetName(remove)
		if err := r.local.Delete(ctx, obj); rresource.IgnoreNotFound(err) != nil {
			return errors.Wrap(err, "cannot delete instance in the local cluster")
		}
	}
	return nil
}
