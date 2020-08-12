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
	"testing"

	"github.com/crossplane/crossplane-runtime/apis/core/v1alpha1"

	v1 "k8s.io/api/core/v1"

	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"

	"github.com/crossplane/crossplane-runtime/pkg/resource"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"

	"github.com/google/go-cmp/cmp"
	"github.com/pkg/errors"
	kerrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/manager"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"

	"github.com/crossplane/crossplane-runtime/pkg/resource/fake"
	"github.com/crossplane/crossplane-runtime/pkg/resource/unstructured/requirement"
	"github.com/crossplane/crossplane-runtime/pkg/test"
)

var (
	errBoom = errors.New("boom")
	now     = metav1.Now()
)

func TestReconcile(t *testing.T) {
	type args struct {
		m      manager.Manager
		remote client.Client
		opts   []ReconcilerOption
		in     *requirement.Unstructured
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
		"LocalGetFailed": {
			reason: "An error should be returned if local requirement cannot be retrieved",
			args: args{
				m: &fake.Manager{
					Client: &test.MockClient{MockGet: test.NewMockGetFn(errBoom)},
				},
			},
			want: want{
				result: reconcile.Result{RequeueAfter: shortWait},
				err:    errors.Wrap(errBoom, localPrefix+errGetRequirement),
			},
		},
		"NotFound": {
			reason: "No error should be returned if local requirement is gone",
			args: args{
				m: &fake.Manager{
					Client: &test.MockClient{MockGet: test.NewMockGetFn(kerrors.NewNotFound(schema.GroupResource{}, ""))},
				},
			},
		},
		"RemoteGetFailed": {
			reason: "An error should be returned if remote requirement cannot be retrieved",
			args: args{
				m: &fake.Manager{
					Client: &test.MockClient{MockGet: test.NewMockGetFn(nil)},
				},
				remote: &test.MockClient{MockGet: test.NewMockGetFn(errBoom)},
			},
			want: want{
				result: reconcile.Result{RequeueAfter: shortWait},
				err:    errors.Wrap(errBoom, remotePrefix+errGetRequirement),
			},
		},
		"RemoteNotFoundAndDeleted": {
			reason: "No error should be returned if deletion is requested and the remote requirement is gone",
			args: args{
				m: &fake.Manager{
					Client: &test.MockClient{MockGet: func(_ context.Context, _ client.ObjectKey, obj runtime.Object) error {
						l := requirement.New()
						l.SetDeletionTimestamp(&now)
						l.DeepCopyInto(obj.(*unstructured.Unstructured))
						return nil
					}},
				},
				remote: &test.MockClient{MockGet: test.NewMockGetFn(kerrors.NewNotFound(schema.GroupResource{}, ""))},
				opts: []ReconcilerOption{
					WithFinalizer(resource.FinalizerFns{RemoveFinalizerFn: func(_ context.Context, _ resource.Object) error {
						return nil
					}}),
				},
			},
		},
		"RemoveFinalizerFailed": {
			reason: "Error during finalizer removal should be propagated",
			args: args{
				m: &fake.Manager{
					Client: &test.MockClient{MockGet: func(_ context.Context, _ client.ObjectKey, obj runtime.Object) error {
						l := requirement.New()
						l.SetDeletionTimestamp(&now)
						l.DeepCopyInto(obj.(*unstructured.Unstructured))
						return nil
					}},
				},
				remote: &test.MockClient{MockGet: test.NewMockGetFn(kerrors.NewNotFound(schema.GroupResource{}, ""))},
				opts: []ReconcilerOption{
					WithFinalizer(resource.FinalizerFns{RemoveFinalizerFn: func(_ context.Context, _ resource.Object) error {
						return errBoom
					}}),
				},
			},
			want: want{
				err:    errors.Wrap(errBoom, localPrefix+errRemoveFinalizer),
				result: reconcile.Result{RequeueAfter: shortWait},
			},
		},
		"RemoteFoundAndDeletionFailed": {
			reason: "The error should be returned if deletion call fails",
			args: args{
				m: &fake.Manager{
					Client: &test.MockClient{MockGet: func(_ context.Context, _ client.ObjectKey, obj runtime.Object) error {
						l := requirement.New()
						l.SetDeletionTimestamp(&now)
						l.DeepCopyInto(obj.(*unstructured.Unstructured))
						return nil
					}},
				},
				remote: &test.MockClient{
					MockGet:    test.NewMockGetFn(nil),
					MockDelete: test.NewMockDeleteFn(errBoom),
				},
				opts: []ReconcilerOption{
					WithFinalizer(resource.FinalizerFns{}),
				},
			},
			want: want{
				err:    errors.Wrap(errBoom, remotePrefix+errDeleteRequirement),
				result: reconcile.Result{RequeueAfter: shortWait},
			},
		},
		"RemoteFoundAndDeletionCalled": {
			reason: "No error should be returned when deletion is requested",
			args: args{
				m: &fake.Manager{
					Client: &test.MockClient{MockGet: func(_ context.Context, _ client.ObjectKey, obj runtime.Object) error {
						l := requirement.New()
						l.SetDeletionTimestamp(&now)
						l.DeepCopyInto(obj.(*unstructured.Unstructured))
						return nil
					}},
				},
				remote: &test.MockClient{
					MockGet:    test.NewMockGetFn(nil),
					MockDelete: test.NewMockDeleteFn(nil),
				},
				opts: []ReconcilerOption{
					WithFinalizer(resource.FinalizerFns{}),
				},
			},
			want: want{
				result: reconcile.Result{RequeueAfter: tinyWait},
			},
		},
		"AddFinalizerFailed": {
			reason: "An error should be returned if finalizer cannot be added",
			args: args{
				m: &fake.Manager{
					Client: &test.MockClient{MockGet: test.NewMockGetFn(nil)},
				},
				remote: &test.MockClient{MockGet: test.NewMockGetFn(nil)},
				opts: []ReconcilerOption{
					WithFinalizer(resource.FinalizerFns{AddFinalizerFn: func(_ context.Context, _ resource.Object) error {
						return errBoom
					}}),
				},
			},
			want: want{
				result: reconcile.Result{RequeueAfter: shortWait},
				err:    errors.Wrap(errBoom, localPrefix+errAddFinalizer),
			},
		},
		"RemoteApplyFailed": {
			reason: "An error should be returned if remote requirement cannot be applied",
			args: args{
				m: &fake.Manager{
					Client: &test.MockClient{MockGet: test.NewMockGetFn(nil)},
				},
				remote: &test.MockClient{
					MockGet:    test.NewMockGetFn(kerrors.NewNotFound(schema.GroupResource{}, "")),
					MockCreate: test.NewMockCreateFn(errBoom),
				},
				opts: []ReconcilerOption{
					WithFinalizer(resource.FinalizerFns{AddFinalizerFn: func(_ context.Context, _ resource.Object) error {
						return nil
					}}),
				},
			},
			want: want{
				result: reconcile.Result{RequeueAfter: shortWait},
				err:    errors.Wrap(errors.Wrap(errBoom, "cannot create object"), remotePrefix+errApplyRequirement),
			},
		},
		"LocalUpdateFailed": {
			reason: "An error should be returned if local requirement cannot be updated with late-inited params",
			args: args{
				m: &fake.Manager{
					Client: &test.MockClient{
						MockGet:    test.NewMockGetFn(nil),
						MockUpdate: test.NewMockUpdateFn(errBoom),
					},
				},
				remote: &test.MockClient{
					MockGet:    test.NewMockGetFn(kerrors.NewNotFound(schema.GroupResource{}, "")),
					MockCreate: test.NewMockCreateFn(nil),
				},
				opts: []ReconcilerOption{
					WithFinalizer(resource.FinalizerFns{AddFinalizerFn: func(_ context.Context, _ resource.Object) error {
						return nil
					}}),
				},
			},
			want: want{
				result: reconcile.Result{RequeueAfter: shortWait},
				err:    errors.Wrap(errBoom, localPrefix+errUpdateRequirement),
			},
		},
		"LocalStatusUpdateFailed": {
			reason: "An error should be returned if status of local requirement cannot be updated",
			args: args{
				m: &fake.Manager{
					Client: &test.MockClient{
						MockGet:          test.NewMockGetFn(nil),
						MockUpdate:       test.NewMockUpdateFn(nil),
						MockStatusUpdate: test.NewMockStatusUpdateFn(errBoom),
					},
				},
				remote: &test.MockClient{
					MockGet:    test.NewMockGetFn(kerrors.NewNotFound(schema.GroupResource{}, "")),
					MockCreate: test.NewMockCreateFn(nil),
				},
				opts: []ReconcilerOption{
					WithFinalizer(resource.FinalizerFns{AddFinalizerFn: func(_ context.Context, _ resource.Object) error {
						return nil
					}}),
				},
			},
			want: want{
				result: reconcile.Result{RequeueAfter: shortWait},
				err:    errors.Wrap(errBoom, localPrefix+errStatusUpdateRequirement),
			},
		},
		"SuccessfulWithNoSecretRef": {
			reason: "No error should be returned if requirement reconciles and does not have a connection secret",
			args: args{
				m: &fake.Manager{
					Client: &test.MockClient{
						MockGet:          test.NewMockGetFn(nil),
						MockUpdate:       test.NewMockUpdateFn(nil),
						MockStatusUpdate: test.NewMockStatusUpdateFn(nil),
					},
				},
				remote: &test.MockClient{
					MockGet:    test.NewMockGetFn(kerrors.NewNotFound(schema.GroupResource{}, "")),
					MockCreate: test.NewMockCreateFn(nil),
				},
				opts: []ReconcilerOption{
					WithFinalizer(resource.FinalizerFns{AddFinalizerFn: func(_ context.Context, _ resource.Object) error {
						return nil
					}}),
				},
			},
			want: want{
				result: reconcile.Result{RequeueAfter: longWait},
			},
		},
		"RemoteGetSecretFailed": {
			reason: "The error should be returned if connection secret in the remote cannot be fetched",
			args: args{
				m: &fake.Manager{
					Client: &test.MockClient{
						MockGet:          test.NewMockGetFn(nil),
						MockUpdate:       test.NewMockUpdateFn(nil),
						MockStatusUpdate: test.NewMockStatusUpdateFn(nil),
					},
				},
				remote: &test.MockClient{
					MockGet: func(_ context.Context, _ client.ObjectKey, obj runtime.Object) error {
						switch obj.(type) {
						case *v1.Secret:
							return errBoom
						case *unstructured.Unstructured:
							r := requirement.New()
							r.SetWriteConnectionSecretToReference(&v1alpha1.LocalSecretReference{Name: "ola"})
							r.DeepCopyInto(obj.(*unstructured.Unstructured))
							return nil
						}
						return errBoom
					},
					MockPatch: test.NewMockPatchFn(nil),
				},
				opts: []ReconcilerOption{
					WithFinalizer(resource.FinalizerFns{AddFinalizerFn: func(_ context.Context, _ resource.Object) error {
						return nil
					}}),
				},
			},
			want: want{
				result: reconcile.Result{RequeueAfter: shortWait},
				err:    errors.Wrap(errBoom, remotePrefix+errGetSecret),
			},
		},
		"RemoteSecretNotReadyYet": {
			reason: "No error should be returned if connection secret in the remote is not published yet",
			args: args{
				m: &fake.Manager{
					Client: &test.MockClient{
						MockGet:          test.NewMockGetFn(nil),
						MockUpdate:       test.NewMockUpdateFn(nil),
						MockStatusUpdate: test.NewMockStatusUpdateFn(nil),
					},
				},
				remote: &test.MockClient{
					MockGet: func(_ context.Context, _ client.ObjectKey, obj runtime.Object) error {
						switch obj.(type) {
						case *v1.Secret:
							return kerrors.NewNotFound(schema.GroupResource{}, "")
						case *unstructured.Unstructured:
							r := requirement.New()
							r.SetWriteConnectionSecretToReference(&v1alpha1.LocalSecretReference{Name: "ola"})
							r.DeepCopyInto(obj.(*unstructured.Unstructured))
							return nil
						}
						return errBoom
					},
					MockPatch: test.NewMockPatchFn(nil),
				},
				opts: []ReconcilerOption{
					WithFinalizer(resource.FinalizerFns{AddFinalizerFn: func(_ context.Context, _ resource.Object) error {
						return nil
					}}),
				},
			},
			want: want{
				result: reconcile.Result{RequeueAfter: shortWait},
			},
		},
		"LocalSecretApplyFailed": {
			reason: "The error should be returned if connection secret cannot be applied in local",
			args: args{
				m: &fake.Manager{
					Client: &test.MockClient{
						MockGet: func(_ context.Context, _ client.ObjectKey, obj runtime.Object) error {
							switch obj.(type) {
							case *v1.Secret:
								return errBoom
							case *unstructured.Unstructured:
								return nil
							}
							return nil
						},
						MockUpdate:       test.NewMockUpdateFn(nil),
						MockStatusUpdate: test.NewMockStatusUpdateFn(nil),
					},
				},
				remote: &test.MockClient{
					MockGet: func(_ context.Context, _ client.ObjectKey, obj runtime.Object) error {
						switch obj.(type) {
						case *v1.Secret:
							return nil
						case *unstructured.Unstructured:
							r := requirement.New()
							r.SetWriteConnectionSecretToReference(&v1alpha1.LocalSecretReference{Name: "ola"})
							r.DeepCopyInto(obj.(*unstructured.Unstructured))
							return nil
						}
						return errBoom
					},
					MockPatch: test.NewMockPatchFn(nil),
				},
				opts: []ReconcilerOption{
					WithFinalizer(resource.FinalizerFns{AddFinalizerFn: func(_ context.Context, _ resource.Object) error {
						return nil
					}}),
				},
			},
			want: want{
				result: reconcile.Result{RequeueAfter: shortWait},
				err:    errors.Wrap(errors.Wrap(errBoom, "cannot get object"), localPrefix+errApplySecret),
			},
		},
	}
	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			r := NewReconciler(tc.args.m, tc.args.remote, schema.GroupVersionKind{}, tc.args.opts...)
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
