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

package resource

import (
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/predicate"

	"github.com/crossplane/crossplane/apis/apiextensions/v1alpha1"
)

// NewNameFilter returns a new *NameFilter that uses the given list.
func NewNameFilter(list []types.NamespacedName) predicate.Funcs {
	return predicate.NewPredicateFuncs(func(meta metav1.Object, _ runtime.Object) bool {
		for _, nn := range list {
			if meta.GetName() == nn.Name && meta.GetNamespace() == nn.Namespace {
				return true
			}
		}
		return false
	})
}

// NewXRDWithClaim returns a new XRDWithClaim object.
func NewXRDWithClaim() predicate.Funcs {
	return predicate.NewPredicateFuncs(func(_ metav1.Object, object runtime.Object) bool {
		xrd, ok := object.(*v1alpha1.CompositeResourceDefinition)
		if !ok {
			return true
		}
		return xrd.Spec.ClaimNames != nil
	})
}
