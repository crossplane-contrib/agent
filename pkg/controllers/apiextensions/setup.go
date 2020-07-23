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
	"github.com/crossplane/crossplane-runtime/pkg/logging"
	rresource "github.com/crossplane/crossplane-runtime/pkg/resource"
	"github.com/crossplane/crossplane/apis/apiextensions/v1alpha1"
	"k8s.io/apimachinery/pkg/runtime"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	kcontroller "sigs.k8s.io/controller-runtime/pkg/controller"
)

const (
	maxConcurrency = 5

	infraPubCRDName    = "infrastructurepublications.apiextensions.crossplane.io"
	infraDefCRDName    = "infrastructuredefinitions.apiextensions.crossplane.io"
	compositionCRDName = "compositions.apiextensions.crossplane.io"
)

// SetupInfraDefSync adds a controller that syncs InfrastructureDefinitions.
func SetupInfraDefSync(mgr ctrl.Manager, localClient client.Client, log logging.Logger) error {
	name := "InfrastructureDefinitions"

	nl := func() runtime.Object { return &v1alpha1.InfrastructureDefinitionList{} }
	gi := func(l runtime.Object) []rresource.Object {
		list, _ := l.(*v1alpha1.InfrastructureDefinitionList)
		result := make([]rresource.Object, len(list.Items))
		for i, val := range list.Items {
			obj, _ := val.DeepCopyObject().(rresource.Object)
			result[i] = obj
		}
		return result
	}
	ni := func() rresource.Object { return &v1alpha1.InfrastructureDefinition{} }

	r := NewReconciler(mgr,
		WithLogger(log.WithValues("controller", name)),
		WithCRDName(infraDefCRDName),
		WithNewInstanceFn(ni),
		WithNewListFn(nl),
		WithGetItemsFn(gi),
		WithLocalClient(rresource.ClientApplicator{
			Client:     localClient,
			Applicator: rresource.NewAPIUpdatingApplicator(localClient),
		}))

	return ctrl.NewControllerManagedBy(mgr).
		Named(name).
		For(&v1alpha1.InfrastructureDefinition{}).
		WithOptions(kcontroller.Options{MaxConcurrentReconciles: maxConcurrency}).
		Complete(r)
}

// SetupInfraPubSync adds a controller that reconciles ApplicationConfigurations.
func SetupInfraPubSync(mgr ctrl.Manager, localClient client.Client, log logging.Logger) error {
	name := "InfrastructurePublications"

	nl := func() runtime.Object { return &v1alpha1.InfrastructurePublicationList{} }
	gi := func(l runtime.Object) []rresource.Object {
		list, _ := l.(*v1alpha1.InfrastructurePublicationList)
		result := make([]rresource.Object, len(list.Items))
		for i, val := range list.Items {
			obj, _ := val.DeepCopyObject().(rresource.Object)
			result[i] = obj
		}
		return result
	}
	ni := func() rresource.Object { return &v1alpha1.InfrastructurePublication{} }

	r := NewReconciler(mgr,
		WithLogger(log.WithValues("controller", name)),
		WithCRDName(infraPubCRDName),
		WithNewInstanceFn(ni),
		WithNewListFn(nl),
		WithGetItemsFn(gi),
		WithLocalClient(rresource.ClientApplicator{
			Client:     localClient,
			Applicator: rresource.NewAPIUpdatingApplicator(localClient),
		}))

	return ctrl.NewControllerManagedBy(mgr).
		Named(name).
		For(&v1alpha1.InfrastructurePublication{}).
		WithOptions(kcontroller.Options{MaxConcurrentReconciles: maxConcurrency}).
		Complete(r)
}

// SetupCompositionSync adds a controller that syncs Compositions.
func SetupCompositionSync(mgr ctrl.Manager, localClient client.Client, log logging.Logger) error {
	name := "Compositions"

	nl := func() runtime.Object { return &v1alpha1.CompositionList{} }
	gi := func(l runtime.Object) []rresource.Object {
		list, _ := l.(*v1alpha1.CompositionList)
		result := make([]rresource.Object, len(list.Items))
		for i, val := range list.Items {
			obj, _ := val.DeepCopyObject().(rresource.Object)
			result[i] = obj
		}
		return result
	}
	ni := func() rresource.Object { return &v1alpha1.Composition{} }

	r := NewReconciler(mgr,
		WithLogger(log.WithValues("controller", name)),
		WithCRDName(compositionCRDName),
		WithNewInstanceFn(ni),
		WithNewListFn(nl),
		WithGetItemsFn(gi),
		WithLocalClient(rresource.ClientApplicator{
			Client:     localClient,
			Applicator: rresource.NewAPIUpdatingApplicator(localClient),
		}))

	return ctrl.NewControllerManagedBy(mgr).
		Named(name).
		For(&v1alpha1.Composition{}).
		WithOptions(kcontroller.Options{MaxConcurrentReconciles: maxConcurrency}).
		Complete(r)
}
