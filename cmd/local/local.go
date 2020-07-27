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

	"github.com/crossplane/agent/pkg/controllers/requirement"
	"k8s.io/apimachinery/pkg/runtime/schema"

	"github.com/pkg/errors"
	crds "k8s.io/apiextensions-apiserver/pkg/apis/apiextensions/v1beta1"
	"k8s.io/client-go/rest"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"

	"github.com/crossplane/crossplane-runtime/pkg/logging"
	"github.com/crossplane/crossplane/apis/apiextensions"

	"github.com/crossplane/agent/pkg/controllers/crd"
)

type Agent struct {
	period    time.Duration
	namespace string

	ClusterConfig *rest.Config
	DefaultConfig *rest.Config
}

func (a *Agent) Run(log logging.Logger, period time.Duration) error {
	log.Debug("Starting", "sync-period", period.String())

	// TODO(muvaf): handle the case where no default config is given.
	defaultRemoteClient, err := client.New(a.DefaultConfig, client.Options{})
	if err != nil {
		return errors.Wrap(err, "cannot create default remote client")
	}

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

	if err := crd.SetupRequirementCRD(mgr, clusterRemoteClient, log); err != nil {
		return errors.Wrap(err, "cannot setup requirement crd reconciler")
	}
	gvk := schema.GroupVersionKind{
		Group:   "common.crossplane.io",
		Version: "v1alpha1",
		Kind:    "KubernetesClusterRequirement",
	}
	if err := requirement.SetupRequirement(mgr, defaultRemoteClient, gvk, log); err != nil {
		return errors.Wrap(err, "cannot setup requirement reconciler")
	}

	// TODO(muvaf): A controller engine should be started here and it should spawn
	// a syncer for each new requirement type.

	return errors.Wrap(mgr.Start(ctrl.SetupSignalHandler()), "cannot start controller manager")
}
