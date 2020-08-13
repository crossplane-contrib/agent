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
package publication

import (
	"fmt"

	"k8s.io/apiextensions-apiserver/pkg/apis/apiextensions/v1beta1"

	"github.com/crossplane/crossplane/apis/apiextensions/v1alpha1"
	"github.com/crossplane/crossplane/apis/apiextensions/v1alpha1/ccrd"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/types"
)

func CRDNameOf(ip v1alpha1.InfrastructurePublication) types.NamespacedName {
	gk := schema.ParseGroupKind(ip.Spec.InfrastructureDefinitionReference.Name)
	if len(gk.Kind) == 0 || len(gk.Group) == 0 {
		return types.NamespacedName{}
	}
	// TODO(muvaf): This isn't a guaranteed way to remove the plurality addition
	// from the Kind since there could be cases where it's not just a suffix of `s`.
	// But we'll remote IP anyway, so, it's good for now.
	return types.NamespacedName{Name: fmt.Sprintf("%s%s.%s", gk.Kind[:len(gk.Kind)-1], ccrd.PublishedInfrastructureSuffixPlural, gk.Group)}
}

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
