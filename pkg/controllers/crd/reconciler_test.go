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

package crd

import (
	"context"
	"testing"

	"k8s.io/apimachinery/pkg/runtime"

	"github.com/crossplane/crossplane-runtime/pkg/resource"

	"github.com/google/go-cmp/cmp"
	"github.com/pkg/errors"
	"k8s.io/apiextensions-apiserver/pkg/apis/apiextensions"
	"sigs.k8s.io/controller-runtime/pkg/manager"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"

	"github.com/crossplane/crossplane-runtime/pkg/logging"
	"github.com/crossplane/crossplane-runtime/pkg/resource/fake"
	"github.com/crossplane/crossplane-runtime/pkg/test"
)

var (
	errBoom = errors.New("boom")
)

func TestReconcile(t *testing.T) {
	type args struct {
		m     manager.Manager
		local resource.ClientApplicator
		in    *apiextensions.CustomResourceDefinition
	}
	type want struct {
		result reconcile.Result
		err    error
	}
	cases := map[string]struct {
		reason string
		args   args
		want   want
	}{
		"SuccessfulApply": {
			reason: "No error should be returned if everything goes as expected",
			args: args{
				m: &fake.Manager{
					Client: &test.MockClient{MockGet: test.NewMockGetFn(nil)},
				},
				local: resource.ClientApplicator{
					Applicator: resource.ApplyFn(func(_ context.Context, _ runtime.Object, _ ...resource.ApplyOption) error {
						return nil
					}),
				},
				in: &apiextensions.CustomResourceDefinition{},
			},
			want: want{result: reconcile.Result{RequeueAfter: longWait}},
		},
		"RemoteGetFailed": {
			args: args{
				m: &fake.Manager{
					Client: &test.MockClient{MockGet: test.NewMockGetFn(errBoom)},
				},
				in: &apiextensions.CustomResourceDefinition{},
			},
			want: want{
				result: reconcile.Result{RequeueAfter: shortWait},
				err:    errors.Wrap(errBoom, remote+errGetCRD),
			},
		},
		"ApplyFailed": {
			args: args{
				m: &fake.Manager{
					Client: &test.MockClient{MockGet: test.NewMockGetFn(nil)},
				},
				local: resource.ClientApplicator{
					Applicator: resource.ApplyFn(func(_ context.Context, _ runtime.Object, _ ...resource.ApplyOption) error {
						return errBoom
					}),
				},
				in: &apiextensions.CustomResourceDefinition{},
			},
			want: want{
				result: reconcile.Result{RequeueAfter: longWait},
				err:    errors.Wrap(errBoom, local+errApplyCRD),
			},
		},
	}
	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			r := NewReconciler(tc.args.m, tc.args.local, logging.NewNopLogger())
			got, err := r.Reconcile(reconcile.Request{})

			if diff := cmp.Diff(tc.want.err, err, test.EquateErrors()); diff != "" {
				t.Errorf("\nReason: %s\nr.Reconcile(...): -want error, +got error:\n%s", tc.reason, diff)
			}

			if diff := cmp.Diff(tc.want.result, got); diff != "" {
				t.Errorf("\nReason: %s\nr.Reconcile(...): -want, +got:\n%s", tc.reason, diff)
			}
		})
	}
}
