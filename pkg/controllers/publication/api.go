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

	"k8s.io/apiextensions-apiserver/pkg/apis/apiextensions/v1beta1"
	"sigs.k8s.io/controller-runtime/pkg/client"

	"github.com/crossplane/crossplane/apis/apiextensions/v1alpha1"

	"github.com/crossplane/agent/pkg/resource"
)

// NewNopRenderer returns a NopRenderer.
func NewNopRenderer() NopRenderer {
	return NopRenderer{}
}

// NopRenderer does nothing.
type NopRenderer struct{}

// Render does nothing.
func (n NopRenderer) Render(_ context.Context, _ v1alpha1.InfrastructurePublication) (*v1beta1.CustomResourceDefinition, error) {
	return nil, nil
}

// RenderFn is used to provide a single function instead of a full object to satisfy
// Render interface.
type RenderFn func(ctx context.Context, ip v1alpha1.InfrastructurePublication) (*v1beta1.CustomResourceDefinition, error)

// Render calls RenderFn it belongs to.
func (r RenderFn) Render(ctx context.Context, ip v1alpha1.InfrastructurePublication) (*v1beta1.CustomResourceDefinition, error) {
	return r(ctx, ip)
}

// NewAPIRemoteCRDRenderer returns a new APIRemoteCRDRenderer.
func NewAPIRemoteCRDRenderer(client client.Client) *APIRemoteCRDRenderer {
	return &APIRemoteCRDRenderer{
		client: client,
	}
}

// APIRemoteCRDRenderer renders the referenced CRD by fetching it from given
// cluster and cleaning up the metadata enough that it can be created in another
// cluster.
type APIRemoteCRDRenderer struct {
	client client.Client
}

// Render returns the sanitized form of the CRD of given InfrastructurePublication
// by fetching it from remote cluster and stripping out cluster-specific metadata.
func (r *APIRemoteCRDRenderer) Render(ctx context.Context, ip v1alpha1.InfrastructurePublication) (*v1beta1.CustomResourceDefinition, error) {
	remote := &v1beta1.CustomResourceDefinition{}
	if err := r.client.Get(ctx, CRDNameOf(ip), remote); err != nil {
		return nil, err
	}
	return resource.SanitizedDeepCopyObject(remote).(*v1beta1.CustomResourceDefinition), nil
}
