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
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"

	"github.com/crossplane/crossplane-runtime/apis/core/v1alpha1"
	"github.com/crossplane/crossplane-runtime/pkg/resource"
)

// Condition constants.
const (
	TypeAgentSync v1alpha1.ConditionType = "AgentSynced"

	ReasonAgentSyncSuccess v1alpha1.ConditionReason = "Success"
	ReasonAgentSyncError   v1alpha1.ConditionReason = "Error"
)

// SanitizedDeepCopyObject removes the metadata that can be specific to a cluster.
// For example, owner references are references to resources in that cluster and
// would be meaningless in another one.
func SanitizedDeepCopyObject(in runtime.Object) resource.Object {
	out, _ := in.DeepCopyObject().(resource.Object)
	out.SetResourceVersion("")
	out.SetUID("")
	out.SetCreationTimestamp(metav1.Time{})
	out.SetSelfLink("")
	out.SetOwnerReferences(nil)
	out.SetManagedFields(nil)
	out.SetFinalizers(nil)
	return out
}

// AgentSyncSuccess returns a condition indicating that Agent successfully
// synced with the remote cluster.
func AgentSyncSuccess() v1alpha1.Condition {
	return v1alpha1.Condition{
		Type:               TypeAgentSync,
		Status:             corev1.ConditionTrue,
		LastTransitionTime: metav1.Now(),
		Reason:             ReasonAgentSyncSuccess,
	}
}

// AgentSyncError returns a condition indicating that Agent encountered an
// error while syncing the resource.
func AgentSyncError(err error) v1alpha1.Condition {
	return v1alpha1.Condition{
		Type:               TypeAgentSync,
		Status:             corev1.ConditionFalse,
		LastTransitionTime: metav1.Now(),
		Reason:             ReasonAgentSyncError,
		Message:            err.Error(),
	}
}
