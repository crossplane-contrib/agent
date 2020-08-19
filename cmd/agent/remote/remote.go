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

package remote

import (
	"time"

	"github.com/pkg/errors"
	crds "k8s.io/apiextensions-apiserver/pkg/apis/apiextensions/v1beta1"
	"k8s.io/client-go/rest"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/manager"

	"github.com/crossplane/crossplane-runtime/pkg/logging"
	capiextensions "github.com/crossplane/crossplane/apis/apiextensions"

	"github.com/crossplane/agent/pkg/controllers/apiextensions"
	"github.com/crossplane/agent/pkg/controllers/crd"
)

// Agent configures & starts the manager that is watching the remote cluster.
type Agent struct {
	ClusterConfig *rest.Config
	DefaultConfig *rest.Config
}

// Run adds all controllers and starts the manager that watches the remote cluster.
func (a *Agent) Run(log logging.Logger, period time.Duration) error {
	log.Debug("Starting", "sync-period", period.String())

	localClient, err := client.New(ctrl.GetConfigOrDie(), client.Options{})
	if err != nil {
		return errors.Wrap(err, "cannot create local client")
	}

	mgr, err := ctrl.NewManager(a.ClusterConfig, ctrl.Options{SyncPeriod: &period, MetricsBindAddress: "0"})
	if err != nil {
		return errors.Wrap(err, "cannot start remote cluster manager")
	}

	if err := crds.AddToScheme(mgr.GetScheme()); err != nil {
		return errors.Wrap(err, "Cannot add CustomResourceDefinition API to scheme")
	}

	if err := capiextensions.AddToScheme(mgr.GetScheme()); err != nil {
		return errors.Wrap(err, "Cannot add Crossplane apiextensions API to scheme")
	}

	for _, setup := range []func(mgr manager.Manager, localClient client.Client, logger logging.Logger) error{
		crd.Setup,
		apiextensions.SetupInfraPubSync,
		apiextensions.SetupInfraDefSync,
		apiextensions.SetupCompositionSync,
	} {
		if err := setup(mgr, localClient, log); err != nil {
			return errors.Wrap(err, "cannot setup the controller")
		}
	}

	return errors.Wrap(mgr.Start(ctrl.SetupSignalHandler()), "cannot start controller manager")
}
