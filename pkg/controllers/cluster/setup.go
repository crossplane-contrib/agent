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

package cluster

import (
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"

	"github.com/crossplane/crossplane-runtime/pkg/logging"

	"github.com/crossplane/agent/pkg/controllers/cluster/crd"
	"github.com/crossplane/agent/pkg/controllers/cluster/syncer"
)

// SetupInfraPubSync workload controllers.
func Setup(mgr ctrl.Manager, localClient client.Client, l logging.Logger) error {
	for _, setup := range []func(ctrl.Manager, client.Client, logging.Logger) error{
		crd.Setup,
		syncer.SetupInfraPubSync,
		syncer.SetupInfraDefSync,
		syncer.SetupCompositionSync,
	} {
		if err := setup(mgr, localClient, l); err != nil {
			return err
		}
	}
	return nil
}
