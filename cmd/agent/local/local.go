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

package local

import (
	"time"

	"github.com/pkg/errors"
	crds "k8s.io/apiextensions-apiserver/pkg/apis/apiextensions/v1beta1"
	"k8s.io/client-go/rest"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"

	"github.com/crossplane/crossplane-runtime/pkg/logging"
	"github.com/crossplane/crossplane/apis/apiextensions"

	"github.com/crossplane/agent/pkg/controllers/publication"
)

type Agent struct {
	ClusterConfig *rest.Config
	DefaultConfig *rest.Config
}

func (a *Agent) Run(log logging.Logger, period time.Duration) error {
	log.Debug("Starting", "sync-period", period.String())

	clusterRemoteClient, err := client.New(a.ClusterConfig, client.Options{})
	if err != nil {
		return errors.Wrap(err, "cannot create cluster remote client")
	}

	// TODO(muvaf): Since there are 2 containers, their metric bind port should
	// be different.
	mgr, err := ctrl.NewManager(ctrl.GetConfigOrDie(), ctrl.Options{SyncPeriod: &period, MetricsBindAddress: "0"})
	if err != nil {
		return errors.Wrap(err, "cannot start local cluster manager")
	}

	if err := crds.AddToScheme(mgr.GetScheme()); err != nil {
		return errors.Wrap(err, "Cannot add CustomResourceDefinition API to scheme")
	}

	if err := apiextensions.AddToScheme(mgr.GetScheme()); err != nil {
		return errors.Wrap(err, "Cannot add Crossplane apiextensions API to scheme")
	}
	// TODO(muvaf): Need to pass in the default config.
	if err := publication.Setup(mgr, clusterRemoteClient, log); err != nil {
		return errors.Wrap(err, "cannot setup infrastructurepublication reconciler")
	}

	return errors.Wrap(mgr.Start(ctrl.SetupSignalHandler()), "cannot start controller manager")
}
