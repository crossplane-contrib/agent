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
	"context"

	"github.com/pkg/errors"

	"k8s.io/apiextensions-apiserver/pkg/apis/apiextensions/v1beta1"
	"sigs.k8s.io/controller-runtime/pkg/client"

	"github.com/crossplane/crossplane/apis/apiextensions/v1alpha1"

	"github.com/crossplane/agent/pkg/resource"
)

// NewNopFetcher returns a NopFetcher.
func NewNopFetcher() NopFetcher {
	return NopFetcher{}
}

// NopFetcher does nothing.
type NopFetcher struct{}

// Fetch does nothing.
func (n NopFetcher) Fetch(_ context.Context, _ v1alpha1.CompositeResourceDefinition) (*v1beta1.CustomResourceDefinition, error) {
	return nil, nil
}

// FetchFn is used to provide a single function instead of a full object to satisfy
// Fetch interface.
type FetchFn func(ctx context.Context, ip v1alpha1.CompositeResourceDefinition) (*v1beta1.CustomResourceDefinition, error)

// Fetch calls FetchFn it belongs to.
func (r FetchFn) Fetch(ctx context.Context, ip v1alpha1.CompositeResourceDefinition) (*v1beta1.CustomResourceDefinition, error) {
	return r(ctx, ip)
}

// NewAPIRemoteCRDFetcher returns a new APIRemoteCRDFetcher.
func NewAPIRemoteCRDFetcher(client client.Client) *APIRemoteCRDFetcher {
	return &APIRemoteCRDFetcher{
		client: client,
	}
}

// APIRemoteCRDFetcher gets the sanitized form of the claim CRD of given
// CompositeResourceDefinition by fetching it from remote cluster and stripping
// out cluster-specific metadata.
type APIRemoteCRDFetcher struct {
	client client.Client
}

// Fetch returns the sanitized form of the claim CRD of given CompositeResourceDefinition
// by fetching it from remote cluster and stripping out cluster-specific metadata.
func (r *APIRemoteCRDFetcher) Fetch(ctx context.Context, xrd v1alpha1.CompositeResourceDefinition) (*v1beta1.CustomResourceDefinition, error) {
	remote := &v1beta1.CustomResourceDefinition{}
	if err := r.client.Get(ctx, GetClaimCRDName(xrd), remote); err != nil {
		return nil, errors.Wrap(err, errGetCRD)
	}
	return resource.SanitizedDeepCopyObject(remote).(*v1beta1.CustomResourceDefinition), nil
}
