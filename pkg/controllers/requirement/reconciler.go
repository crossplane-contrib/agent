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

package requirement

import (
	"context"
	"time"

	"github.com/crossplane/crossplane-runtime/pkg/resource/unstructured"

	"github.com/crossplane/crossplane-runtime/pkg/resource/unstructured/requirement"
	"k8s.io/apimachinery/pkg/runtime/schema"

	"sigs.k8s.io/controller-runtime/pkg/manager"

	v1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/types"

	"github.com/crossplane/agent/pkg/resource"

	"github.com/pkg/errors"

	"github.com/crossplane/crossplane-runtime/pkg/event"
	"github.com/crossplane/crossplane-runtime/pkg/logging"
	rresource "github.com/crossplane/crossplane-runtime/pkg/resource"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"
)

const (
	timeout   = 2 * time.Minute
	longWait  = 1 * time.Minute
	shortWait = 30 * time.Second
)

func NewReconciler(mgr manager.Manager, remoteClient client.Client, gvk schema.GroupVersionKind, logger logging.Logger) *Reconciler {
	ni := func() rresource.Requirement { return requirement.New(requirement.WithGroupVersionKind(gvk)) }
	return &Reconciler{
		mgr:         mgr,
		local:       unstructured.NewClient(mgr.GetClient()),
		remote:      unstructured.NewClient(remoteClient),
		newInstance: ni,
		log:         logger,
		record:      event.NewAPIRecorder(mgr.GetEventRecorderFor(gvk.GroupKind().String())),
	}
}

type Reconciler struct {
	mgr    ctrl.Manager
	local  client.Client
	remote client.Client

	newInstance func() rresource.Requirement

	log    logging.Logger
	record event.Recorder
}

func (r *Reconciler) Reconcile(req reconcile.Request) (reconcile.Result, error) {
	log := r.log.WithValues("request", req)
	log.Debug("Reconciling")

	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()

	re := r.newInstance()
	if err := r.local.Get(ctx, req.NamespacedName, re); err != nil {
		return reconcile.Result{RequeueAfter: shortWait}, errors.Wrap(err, "cannot get requirement in the local cluster")
	}
	reRemote := r.newInstance()
	if err := r.remote.Get(ctx, req.NamespacedName, re); rresource.IgnoreNotFound(err) != nil {
		return reconcile.Result{RequeueAfter: shortWait}, errors.Wrap(err, "cannot get requirement in the remote cluster")
	}
	resource.EqualizeMetadata(reRemote, re)
	if err := r.remote.Update(ctx, re); err != nil {
		return reconcile.Result{RequeueAfter: shortWait}, errors.Wrap(err, "cannot update requirement in the remote cluster")
	}
	if re.GetWriteConnectionSecretToReference() == nil {
		return reconcile.Result{RequeueAfter: shortWait}, nil
	}
	rs := &v1.Secret{}
	rnn := types.NamespacedName{
		Name:      reRemote.GetWriteConnectionSecretToReference().Name,
		Namespace: reRemote.GetNamespace(),
	}
	if err := r.remote.Get(ctx, rnn, rs); err != nil {
		return reconcile.Result{RequeueAfter: shortWait}, errors.Wrap(err, "cannot get secret of the requirement from the remote cluster")
	}
	ls := &v1.Secret{}
	lnn := types.NamespacedName{
		Name:      re.GetWriteConnectionSecretToReference().Name,
		Namespace: re.GetNamespace(),
	}
	if err := r.local.Get(ctx, lnn, ls); err != nil {
		return reconcile.Result{RequeueAfter: shortWait}, errors.Wrap(err, "cannot get secret of the requirement from the local cluster")
	}
	resource.EqualizeMetadata(ls, rs)
	rs.SetNamespace(re.GetNamespace())
	if err := r.local.Update(ctx, rs); err != nil {
		return reconcile.Result{RequeueAfter: shortWait}, errors.Wrap(err, "cannot update secret of the requirement in the local cluster")
	}
	return reconcile.Result{RequeueAfter: longWait}, nil
}
