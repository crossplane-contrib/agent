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

package xrd

import (
	"fmt"

	"k8s.io/apiextensions-apiserver/pkg/apis/apiextensions/v1beta1"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/types"

	"github.com/crossplane/crossplane/apis/apiextensions/v1alpha1"
)

// GetClaimCRDName returns the name of the claim CRD that's created as result of
// given CompositeResourceDefinition.
func GetClaimCRDName(xrd v1alpha1.CompositeResourceDefinition) types.NamespacedName {
	if xrd.Spec.ClaimNames == nil {
		return types.NamespacedName{}
	}
	return types.NamespacedName{Name: fmt.Sprintf("%s.%s", xrd.Spec.ClaimNames.Plural, xrd.Spec.CRDSpecTemplate.Group)}
}

// GroupVersionKindOf returns the served GroupVersionKind of given CRD.
func GroupVersionKindOf(crd v1beta1.CustomResourceDefinition) schema.GroupVersionKind {
	servedVersion := crd.Spec.Version
	for _, v := range crd.Spec.Versions {
		if v.Served {
			servedVersion = v.Name
			break
		}
	}
	return schema.GroupVersionKind{
		Group:   crd.Spec.Group,
		Kind:    crd.Spec.Names.Kind,
		Version: servedVersion,
	}
}
