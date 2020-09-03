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
	"k8s.io/apimachinery/pkg/runtime"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	kcontroller "sigs.k8s.io/controller-runtime/pkg/controller"

	"github.com/crossplane/crossplane-runtime/pkg/logging"
	runtimeresource "github.com/crossplane/crossplane-runtime/pkg/resource"
	"github.com/crossplane/crossplane/apis/apiextensions/v1alpha1"
)

const (
	maxConcurrency = 5

	xrdCRDName         = "compositeresourcedefinitions.apiextensions.crossplane.io"
	compositionCRDName = "compositions.apiextensions.crossplane.io"
)

// SetupXRDSync adds a controller that syncs CompositeResourceDefinitions from
// remote cluster to local cluster.
func SetupXRDSync(mgr ctrl.Manager, localClient client.Client, log logging.Logger) error {
	name := "CompositeResourceDefinitions"

	nl := func() runtime.Object { return &v1alpha1.CompositeResourceDefinitionList{} }
	gi := func(l runtime.Object) []runtimeresource.Object {
		list, _ := l.(*v1alpha1.CompositeResourceDefinitionList)
		result := make([]runtimeresource.Object, len(list.Items))
		for i, val := range list.Items {
			obj, _ := val.DeepCopyObject().(runtimeresource.Object)
			result[i] = obj
		}
		return result
	}
	ni := func() runtimeresource.Object { return &v1alpha1.CompositeResourceDefinition{} }
	ca := runtimeresource.ClientApplicator{
		Client:     localClient,
		Applicator: runtimeresource.NewAPIPatchingApplicator(localClient),
	}

	r := NewReconciler(mgr,
		ca,
		WithLogger(log.WithValues("controller", name)),
		WithCRDName(xrdCRDName),
		WithNewInstanceFn(ni),
		WithNewObjectListFn(nl),
		WithGetItemsFn(gi))

	return ctrl.NewControllerManagedBy(mgr).
		Named(name).
		For(&v1alpha1.CompositeResourceDefinition{}).
		WithOptions(kcontroller.Options{MaxConcurrentReconciles: maxConcurrency}).
		Complete(r)
}

// SetupCompositionSync adds a controller that syncs Compositions from
// remote cluster to local cluster.
func SetupCompositionSync(mgr ctrl.Manager, localClient client.Client, log logging.Logger) error {
	name := "Compositions"

	nl := func() runtime.Object { return &v1alpha1.CompositionList{} }
	gi := func(l runtime.Object) []runtimeresource.Object {
		list, _ := l.(*v1alpha1.CompositionList)
		result := make([]runtimeresource.Object, len(list.Items))
		for i, val := range list.Items {
			obj, _ := val.DeepCopyObject().(runtimeresource.Object)
			result[i] = obj
		}
		return result
	}
	ni := func() runtimeresource.Object { return &v1alpha1.Composition{} }
	ca := runtimeresource.ClientApplicator{
		Client:     localClient,
		Applicator: runtimeresource.NewAPIPatchingApplicator(localClient),
	}

	r := NewReconciler(mgr,
		ca,
		WithLogger(log.WithValues("controller", name)),
		WithCRDName(compositionCRDName),
		WithNewInstanceFn(ni),
		WithNewObjectListFn(nl),
		WithGetItemsFn(gi))

	return ctrl.NewControllerManagedBy(mgr).
		Named(name).
		For(&v1alpha1.Composition{}).
		WithOptions(kcontroller.Options{MaxConcurrentReconciles: maxConcurrency}).
		Complete(r)
}
