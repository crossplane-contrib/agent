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
	"testing"

	"github.com/google/go-cmp/cmp"
	"github.com/pkg/errors"
	apiextensions "k8s.io/apiextensions-apiserver/pkg/apis/apiextensions/v1beta1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	kunstructured "k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	kcontroller "sigs.k8s.io/controller-runtime/pkg/controller"
	"sigs.k8s.io/controller-runtime/pkg/manager"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"

	"github.com/crossplane/crossplane-runtime/pkg/controller"
	"github.com/crossplane/crossplane-runtime/pkg/resource"
	"github.com/crossplane/crossplane-runtime/pkg/resource/fake"
	"github.com/crossplane/crossplane-runtime/pkg/test"
	"github.com/crossplane/crossplane/apis/apiextensions/v1alpha1"
)

var (
	errBoom = errors.New("boom")
	now     = metav1.Now()
	trueVal = true
)

type MockEngine struct {
	MockStart func(name string, o kcontroller.Options, w ...controller.Watch) error
	MockStop  func(name string)
}

func (m *MockEngine) Start(name string, o kcontroller.Options, w ...controller.Watch) error {
	return m.MockStart(name, o, w...)
}
func (m *MockEngine) Stop(name string) {
	m.MockStop(name)
}

func TestReconcile(t *testing.T) {
	type args struct {
		m      manager.Manager
		remote client.Client
		opts   []ReconcilerOption
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
		"GetPubFailed": {
			reason: "An error should be returned if IP cannot be retrieved",
			args: args{
				m: &fake.Manager{
					Client: &test.MockClient{
						MockGet: test.NewMockGetFn(errBoom),
					},
				},
			},
			want: want{
				err:    errors.Wrap(errBoom, localPrefix+errGetPublication),
				result: reconcile.Result{RequeueAfter: shortWait},
			},
		},
		"RenderFailed": {
			reason: "An error should be returned if CRD cannot be rendered",
			args: args{
				m: &fake.Manager{
					Client: &test.MockClient{
						MockGet: test.NewMockGetFn(nil),
					},
				},
				opts: []ReconcilerOption{
					WithCRDRenderer(RenderFn(func(_ context.Context, _ v1alpha1.InfrastructurePublication) (*apiextensions.CustomResourceDefinition, error) {
						return nil, errBoom
					})),
				},
			},
			want: want{
				err:    errors.Wrap(errBoom, remotePrefix+errRenderCRD),
				result: reconcile.Result{RequeueAfter: shortWait},
			},
		},
		"LocalGetCRDFailed": {
			reason: "An error should be returned if we cannot get the local CRD during deletion",
			args: args{
				m: &fake.Manager{
					Client: &test.MockClient{
						MockGet: func(_ context.Context, _ client.ObjectKey, obj runtime.Object) error {
							switch o := obj.(type) {
							case *apiextensions.CustomResourceDefinition:
								return errBoom
							case *v1alpha1.InfrastructurePublication:
								ip := &v1alpha1.InfrastructurePublication{
									ObjectMeta: metav1.ObjectMeta{
										DeletionTimestamp: &now,
									},
								}
								ip.DeepCopyInto(o)
							}
							return nil
						},
					},
				},
			},
			want: want{
				err:    errors.Wrap(errBoom, localPrefix+errGetCRD),
				result: reconcile.Result{RequeueAfter: shortWait},
			},
		},
		"RemoveFinalizerFailed": {
			reason: "An error should be returned if we cannot remove finalizer from IP after a successful CRD deletion",
			args: args{
				m: &fake.Manager{
					Client: &test.MockClient{
						MockGet: func(_ context.Context, _ client.ObjectKey, obj runtime.Object) error {
							if o, ok := obj.(*v1alpha1.InfrastructurePublication); ok {
								ip := &v1alpha1.InfrastructurePublication{
									ObjectMeta: metav1.ObjectMeta{
										DeletionTimestamp: &now,
									},
								}
								ip.DeepCopyInto(o)
							}
							return nil
						},
					},
				},
				opts: []ReconcilerOption{
					WithFinalizer(resource.FinalizerFns{
						RemoveFinalizerFn: func(_ context.Context, _ resource.Object) error {
							return errBoom
						},
					}),
					WithCRDRenderer(RenderFn(func(_ context.Context, _ v1alpha1.InfrastructurePublication) (*apiextensions.CustomResourceDefinition, error) {
						return &apiextensions.CustomResourceDefinition{}, nil
					})),
				},
			},
			want: want{
				err:    errors.Wrap(errBoom, localPrefix+errRemoveFinalizer),
				result: reconcile.Result{RequeueAfter: shortWait},
			},
		},
		"RemoveFinalizerSuccess": {
			reason: "We should not return error or requeue if finalizer of CRD is removed",
			args: args{
				m: &fake.Manager{
					Client: &test.MockClient{
						MockGet: func(_ context.Context, _ client.ObjectKey, obj runtime.Object) error {
							if o, ok := obj.(*v1alpha1.InfrastructurePublication); ok {
								ip := &v1alpha1.InfrastructurePublication{
									ObjectMeta: metav1.ObjectMeta{
										DeletionTimestamp: &now,
									},
								}
								ip.DeepCopyInto(o)
							}
							return nil
						},
					},
				},
				opts: []ReconcilerOption{
					WithFinalizer(resource.FinalizerFns{
						RemoveFinalizerFn: func(_ context.Context, _ resource.Object) error {
							return nil
						},
					}),
					WithCRDRenderer(RenderFn(func(_ context.Context, _ v1alpha1.InfrastructurePublication) (*apiextensions.CustomResourceDefinition, error) {
						return &apiextensions.CustomResourceDefinition{}, nil
					})),
				},
			},
			want: want{
				result: reconcile.Result{Requeue: false},
			},
		},
		"CustomResourceListFailed": {
			reason: "We should return the error if custom resources cannot be listed",
			args: args{
				m: &fake.Manager{
					Client: &test.MockClient{
						MockGet: func(_ context.Context, _ client.ObjectKey, obj runtime.Object) error {
							if o, ok := obj.(*v1alpha1.InfrastructurePublication); ok {
								ip := &v1alpha1.InfrastructurePublication{
									ObjectMeta: metav1.ObjectMeta{
										DeletionTimestamp: &now,
										UID:               "ola",
									},
								}
								ip.DeepCopyInto(o)
							}
							return nil
						},
						MockList: test.NewMockListFn(errBoom),
					},
				},
				opts: []ReconcilerOption{
					WithCRDRenderer(RenderFn(func(_ context.Context, _ v1alpha1.InfrastructurePublication) (*apiextensions.CustomResourceDefinition, error) {
						return &apiextensions.CustomResourceDefinition{
							ObjectMeta: metav1.ObjectMeta{
								CreationTimestamp: now,
								OwnerReferences: []metav1.OwnerReference{
									{
										UID:        "ola",
										Controller: &trueVal,
									},
								},
							},
						}, nil
					})),
				},
			},
			want: want{
				err:    errors.Wrap(errBoom, localPrefix+errListCR),
				result: reconcile.Result{RequeueAfter: shortWait},
			},
		},
		"CustomResourceDeleteFailed": {
			reason: "We should return the error if custom resources cannot be deleted",
			args: args{
				m: &fake.Manager{
					Client: &test.MockClient{
						MockGet: func(_ context.Context, _ client.ObjectKey, obj runtime.Object) error {
							if o, ok := obj.(*v1alpha1.InfrastructurePublication); ok {
								ip := &v1alpha1.InfrastructurePublication{
									ObjectMeta: metav1.ObjectMeta{
										DeletionTimestamp: &now,
										UID:               "ola",
									},
								}
								ip.DeepCopyInto(o)
							}
							return nil
						},
						MockList: func(_ context.Context, list runtime.Object, _ ...client.ListOption) error {
							l := &kunstructured.UnstructuredList{Items: []kunstructured.Unstructured{{}}}
							l.DeepCopyInto(list.(*kunstructured.UnstructuredList))
							return nil
						},
						MockDelete: test.NewMockDeleteFn(errBoom),
					},
				},
				opts: []ReconcilerOption{
					WithCRDRenderer(RenderFn(func(_ context.Context, _ v1alpha1.InfrastructurePublication) (*apiextensions.CustomResourceDefinition, error) {
						return &apiextensions.CustomResourceDefinition{
							ObjectMeta: metav1.ObjectMeta{
								CreationTimestamp: now,
								OwnerReferences: []metav1.OwnerReference{
									{
										UID:        "ola",
										Controller: &trueVal,
									},
								},
							},
						}, nil
					})),
				},
			},
			want: want{
				err:    errors.Wrap(errBoom, localPrefix+errDeleteCR),
				result: reconcile.Result{RequeueAfter: shortWait},
			},
		},
		"CustomResourceDeleteSuccessful": {
			reason: "We should return the error if custom resources cannot be deleted",
			args: args{
				m: &fake.Manager{
					Client: &test.MockClient{
						MockGet: func(_ context.Context, _ client.ObjectKey, obj runtime.Object) error {
							if o, ok := obj.(*v1alpha1.InfrastructurePublication); ok {
								ip := &v1alpha1.InfrastructurePublication{
									ObjectMeta: metav1.ObjectMeta{
										DeletionTimestamp: &now,
										UID:               "ola",
									},
								}
								ip.DeepCopyInto(o)
							}
							return nil
						},
						MockList: func(_ context.Context, list runtime.Object, _ ...client.ListOption) error {
							l := &kunstructured.UnstructuredList{Items: []kunstructured.Unstructured{{}}}
							l.DeepCopyInto(list.(*kunstructured.UnstructuredList))
							return nil
						},
						MockDelete:       test.NewMockDeleteFn(nil),
						MockStatusUpdate: test.NewMockStatusUpdateFn(nil),
					},
				},
				opts: []ReconcilerOption{
					WithCRDRenderer(RenderFn(func(_ context.Context, _ v1alpha1.InfrastructurePublication) (*apiextensions.CustomResourceDefinition, error) {
						return &apiextensions.CustomResourceDefinition{
							ObjectMeta: metav1.ObjectMeta{
								CreationTimestamp: now,
								OwnerReferences: []metav1.OwnerReference{
									{
										UID:        "ola",
										Controller: &trueVal,
									},
								},
							},
						}, nil
					})),
				},
			},
			want: want{
				result: reconcile.Result{RequeueAfter: tinyWait},
			},
		},
		"CRDDeleteFailed": {
			reason: "We should return the error if CRD cannot be deleted",
			args: args{
				m: &fake.Manager{
					Client: &test.MockClient{
						MockGet: func(_ context.Context, _ client.ObjectKey, obj runtime.Object) error {
							if o, ok := obj.(*v1alpha1.InfrastructurePublication); ok {
								ip := &v1alpha1.InfrastructurePublication{
									ObjectMeta: metav1.ObjectMeta{
										DeletionTimestamp: &now,
										UID:               "ola",
									},
								}
								ip.DeepCopyInto(o)
							}
							return nil
						},
						MockList:   test.NewMockListFn(nil),
						MockDelete: test.NewMockDeleteFn(errBoom),
					},
				},
				opts: []ReconcilerOption{
					WithCRDRenderer(RenderFn(func(_ context.Context, _ v1alpha1.InfrastructurePublication) (*apiextensions.CustomResourceDefinition, error) {
						return &apiextensions.CustomResourceDefinition{
							ObjectMeta: metav1.ObjectMeta{
								CreationTimestamp: now,
								OwnerReferences: []metav1.OwnerReference{
									{
										UID:        "ola",
										Controller: &trueVal,
									},
								},
							},
						}, nil
					})),
				},
			},
			want: want{
				err:    errors.Wrap(errBoom, localPrefix+errDeleteCRD),
				result: reconcile.Result{RequeueAfter: shortWait},
			},
		},
		"DeletionSuccessful": {
			reason: "We should not return any error if deletion calls are successful",
			args: args{
				m: &fake.Manager{
					Client: &test.MockClient{
						MockGet: func(_ context.Context, _ client.ObjectKey, obj runtime.Object) error {
							if o, ok := obj.(*v1alpha1.InfrastructurePublication); ok {
								ip := &v1alpha1.InfrastructurePublication{
									ObjectMeta: metav1.ObjectMeta{
										DeletionTimestamp: &now,
										UID:               "ola",
									},
								}
								ip.DeepCopyInto(o)
							}
							return nil
						},
						MockList:         test.NewMockListFn(nil),
						MockDelete:       test.NewMockDeleteFn(nil),
						MockStatusUpdate: test.NewMockStatusUpdateFn(nil),
					},
				},
				opts: []ReconcilerOption{
					WithCRDRenderer(RenderFn(func(_ context.Context, _ v1alpha1.InfrastructurePublication) (*apiextensions.CustomResourceDefinition, error) {
						return &apiextensions.CustomResourceDefinition{
							ObjectMeta: metav1.ObjectMeta{
								CreationTimestamp: now,
								OwnerReferences: []metav1.OwnerReference{
									{
										UID:        "ola",
										Controller: &trueVal,
									},
								},
							},
						}, nil
					})),
				},
			},
			want: want{
				result: reconcile.Result{RequeueAfter: tinyWait},
			},
		},
		"AddFinalizerFailed": {
			reason: "An error should be returned if we cannot add finalizer to IP",
			args: args{
				m: &fake.Manager{
					Client: &test.MockClient{
						MockGet: test.NewMockGetFn(nil),
					},
				},
				opts: []ReconcilerOption{
					WithFinalizer(resource.FinalizerFns{AddFinalizerFn: func(_ context.Context, _ resource.Object) error {
						return errBoom
					}}),
				},
			},
			want: want{
				err:    errors.Wrap(errBoom, localPrefix+errAddFinalizerPub),
				result: reconcile.Result{RequeueAfter: shortWait},
			},
		},
		"ApplyCRDFailed": {
			reason: "An error should be returned if CRD cannot be applied in local cluster",
			args: args{
				m: &fake.Manager{
					Client: &test.MockClient{
						MockGet: test.NewMockGetFn(nil),
					},
				},
				opts: []ReconcilerOption{
					WithLocalApplicator(resource.ApplyFn(func(_ context.Context, _ runtime.Object, _ ...resource.ApplyOption) error {
						return errBoom
					})),
					WithFinalizer(resource.FinalizerFns{AddFinalizerFn: func(_ context.Context, _ resource.Object) error {
						return nil
					}}),
					WithCRDRenderer(RenderFn(func(_ context.Context, _ v1alpha1.InfrastructurePublication) (*apiextensions.CustomResourceDefinition, error) {
						return &apiextensions.CustomResourceDefinition{}, nil
					})),
				},
			},
			want: want{
				err:    errors.Wrap(errBoom, localPrefix+errApplyCRD),
				result: reconcile.Result{RequeueAfter: shortWait},
			},
		},
		"NotEstablishedYet": {
			reason: "Reconciliation should end if CRD is not yet established",
			args: args{
				m: &fake.Manager{
					Client: &test.MockClient{
						MockGet:          test.NewMockGetFn(nil),
						MockStatusUpdate: test.NewMockStatusUpdateFn(nil),
					},
				},
				opts: []ReconcilerOption{
					WithLocalApplicator(resource.ApplyFn(func(_ context.Context, _ runtime.Object, _ ...resource.ApplyOption) error {
						return nil
					})),
					WithFinalizer(resource.FinalizerFns{AddFinalizerFn: func(_ context.Context, _ resource.Object) error {
						return nil
					}}),
					WithCRDRenderer(RenderFn(func(_ context.Context, _ v1alpha1.InfrastructurePublication) (*apiextensions.CustomResourceDefinition, error) {
						return &apiextensions.CustomResourceDefinition{}, nil
					})),
				},
			},
			want: want{
				result: reconcile.Result{RequeueAfter: tinyWait},
			},
		},
		"StartControllerFailed": {
			reason: "The error should be returned if engine cannot start the controller",
			args: args{
				m: &fake.Manager{
					Client: &test.MockClient{
						MockGet: test.NewMockGetFn(nil),
					},
				},
				opts: []ReconcilerOption{
					WithLocalApplicator(resource.ApplyFn(func(_ context.Context, _ runtime.Object, _ ...resource.ApplyOption) error {
						return nil
					})),
					WithFinalizer(resource.FinalizerFns{AddFinalizerFn: func(_ context.Context, _ resource.Object) error {
						return nil
					}}),
					WithCRDRenderer(RenderFn(func(_ context.Context, _ v1alpha1.InfrastructurePublication) (*apiextensions.CustomResourceDefinition, error) {
						return &apiextensions.CustomResourceDefinition{
							Status: apiextensions.CustomResourceDefinitionStatus{
								Conditions: []apiextensions.CustomResourceDefinitionCondition{
									{
										Type:   apiextensions.Established,
										Status: apiextensions.ConditionTrue,
									},
								},
							},
						}, nil
					})),
					WithControllerEngine(&MockEngine{MockStart: func(_ string, _ kcontroller.Options, _ ...controller.Watch) error {
						return errBoom
					}}),
				},
			},
			want: want{
				err:    errors.Wrap(errBoom, localPrefix+errStartController),
				result: reconcile.Result{RequeueAfter: shortWait},
			},
		},
		"Successful": {
			reason: "No error should be returned if all calls go well",
			args: args{
				m: &fake.Manager{
					Client: &test.MockClient{
						MockGet:          test.NewMockGetFn(nil),
						MockStatusUpdate: test.NewMockStatusUpdateFn(nil),
					},
				},
				opts: []ReconcilerOption{
					WithLocalApplicator(resource.ApplyFn(func(_ context.Context, _ runtime.Object, _ ...resource.ApplyOption) error {
						return nil
					})),
					WithFinalizer(resource.FinalizerFns{AddFinalizerFn: func(_ context.Context, _ resource.Object) error {
						return nil
					}}),
					WithCRDRenderer(RenderFn(func(_ context.Context, _ v1alpha1.InfrastructurePublication) (*apiextensions.CustomResourceDefinition, error) {
						return &apiextensions.CustomResourceDefinition{
							Status: apiextensions.CustomResourceDefinitionStatus{
								Conditions: []apiextensions.CustomResourceDefinitionCondition{
									{
										Type:   apiextensions.Established,
										Status: apiextensions.ConditionTrue,
									},
								},
							},
						}, nil
					})),
					WithControllerEngine(&MockEngine{MockStart: func(_ string, _ kcontroller.Options, _ ...controller.Watch) error {
						return nil
					}}),
				},
			},
			want: want{
				result: reconcile.Result{RequeueAfter: longWait},
			},
		},
	}
	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			r := NewReconciler(tc.args.m, tc.args.remote, tc.args.opts...)
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
