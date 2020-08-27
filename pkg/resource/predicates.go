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
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/event"
)

// NewNameFilter returns a new *NameFilter that uses the given list.
func NewNameFilter(list []types.NamespacedName) *NameFilter {
	return &NameFilter{list: list}
}

// NameFilter allows only the objects whose name appears in the list. This applies
// to all kinds of events that a controller can receive.
type NameFilter struct {
	list []types.NamespacedName
}

// Create returns true if the NamespacedName of the object of the event is allowed
// to be reconciled.
func (f *NameFilter) Create(e event.CreateEvent) bool {
	for _, nn := range f.list {
		if e.Meta.GetName() == nn.Name && e.Meta.GetNamespace() == nn.Namespace {
			return true
		}
	}
	return false
}

// Update returns true if the NamespacedName of the object of the event is allowed
// to be reconciled.
func (f *NameFilter) Update(e event.UpdateEvent) bool {
	for _, nn := range f.list {
		if e.MetaNew.GetName() == nn.Name && e.MetaNew.GetNamespace() == nn.Namespace {
			return true
		}
	}
	return false
}

// Delete returns true if the NamespacedName of the object of the event is allowed
// to be reconciled.
func (f *NameFilter) Delete(e event.DeleteEvent) bool {
	for _, nn := range f.list {
		if e.Meta.GetName() == nn.Name && e.Meta.GetNamespace() == nn.Namespace {
			return true
		}
	}
	return false
}

// Generic returns true if the NamespacedName of the object of the event is allowed
// to be reconciled.
func (f *NameFilter) Generic(e event.GenericEvent) bool {
	for _, nn := range f.list {
		if e.Meta.GetName() == nn.Name && e.Meta.GetNamespace() == nn.Namespace {
			return true
		}
	}
	return false
}
