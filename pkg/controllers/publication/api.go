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
	"context"

	"github.com/crossplane/agent/pkg/resource"

	"github.com/crossplane/crossplane/apis/apiextensions/v1alpha1"
	"github.com/pkg/errors"
	"k8s.io/apiextensions-apiserver/pkg/apis/apiextensions/v1beta1"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

func NewNopRenderer() NopRenderer {
	return NopRenderer{}
}

type NopRenderer struct{}

func (n NopRenderer) Render(ctx context.Context, ip *v1alpha1.InfrastructurePublication) (*v1beta1.CustomResourceDefinition, error) {
	return nil, nil
}

type RenderFn func(ctx context.Context, ip *v1alpha1.InfrastructurePublication) (*v1beta1.CustomResourceDefinition, error)

func (r RenderFn) Render(ctx context.Context, ip *v1alpha1.InfrastructurePublication) (*v1beta1.CustomResourceDefinition, error) {
	return r(ctx, ip)
}

func NewAPIRemoteCRDRenderer(client client.Client) *APIRemoteCRDRenderer {
	return &APIRemoteCRDRenderer{
		client: client,
	}
}

type APIRemoteCRDRenderer struct {
	client client.Client
}

func (r *APIRemoteCRDRenderer) Render(ctx context.Context, ip *v1alpha1.InfrastructurePublication) (*v1beta1.CustomResourceDefinition, error) {
	remote := &v1beta1.CustomResourceDefinition{}
	if err := r.client.Get(ctx, CRDNameOf(ip), remote); err != nil {
		return nil, errors.Wrap(err, errGetCRD)
	}
	crd := &v1beta1.CustomResourceDefinition{}
	resource.OverrideInputMetadata(remote, crd)
	remote.Spec.DeepCopyInto(&crd.Spec)
	return crd, nil
}
