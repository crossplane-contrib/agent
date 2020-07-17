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

package main

import (
	"os"
	"path/filepath"
	"time"

	"github.com/crossplane/crossplane/apis/apiextensions"
	"github.com/pkg/errors"
	"gopkg.in/alecthomas/kingpin.v2"
	crds "k8s.io/apiextensions-apiserver/pkg/apis/apiextensions/v1beta1"
	"k8s.io/client-go/rest"
	"k8s.io/client-go/tools/clientcmd"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/log/zap"

	"github.com/crossplane/crossplane-runtime/pkg/logging"

	"github.com/crossplane/agent/pkg/controllers/cluster"
)

func main() {
	var (
		app   = kingpin.New(filepath.Base(os.Args[0]), "A syncer between any cluster and Crossplane instance.").DefaultEnvars()
		debug = app.Flag("debug", "Run with debug logging.").Short('d').Bool()
	)
	// TODO(muvaf): Add flag for ctrl runtime sync duration.
	s := app.Command("sync", "Start syncing to Crossplane.").Default()
	csa := s.Flag("cluster-kubeconfig", "File path of the kubeconfig of ServiceAccount to be used to get cluster-scoped resources like CRDs.").Envar("CLUSTER_KUBECONFIG").String()
	dsa := s.Flag("default-kubeconfig", "File path of the  kubeconfig of ServiceAccount to be used for all namespaces that do not have override annotations.").Envar("DEFAULT_KUBECONFIG").String()
	cmd := kingpin.MustParse(app.Parse(os.Args[1:]))
	if cmd != s.FullCommand() {
		kingpin.FatalUsage("unknown command %s", cmd)
	}
	zl := zap.New(zap.UseDevMode(*debug))
	if *debug {
		// The controller-runtime runs with a no-op logger by default. It is
		// *very* verbose even at info level, so we only provide it a real
		// logger when we're running in debug mode.
		ctrl.SetLogger(zl)
	}
	switch {
	case *csa == "" && *dsa != "":
		*csa = *dsa
	case *csa == "" && *dsa == "":
		kingpin.FatalUsage("cannot get cluster-scoped resources from Crossplane if neither cluster config nor default config is given")
	}

	clusterConfig, err := clientcmd.BuildConfigFromFlags("", *csa)
	if err != nil {
		kingpin.FatalUsage("could not parse cluster kubeconfig %s", *csa)
	}
	var defaultConfig *rest.Config
	if *dsa != "" {
		defaultConfig, err = clientcmd.BuildConfigFromFlags("", *dsa)
		if err != nil {
			kingpin.FatalUsage("could not parse default kubeconfig %s", *csa)
		}
	}
	duration, _ := time.ParseDuration("1h")
	agent := &Agent{
		period:        duration,
		ClusterConfig: clusterConfig,
		DefaultConfig: defaultConfig,
	}

	kingpin.FatalIfError(agent.Run(logging.NewLogrLogger(zl.WithName("crossplane-agent")), duration), "cannot run agent")
}

type Agent struct {
	period    time.Duration
	namespace string

	ClusterConfig *rest.Config
	DefaultConfig *rest.Config
}

func (a *Agent) Run(log logging.Logger, period time.Duration) error {
	log.Debug("Starting", "sync-period", period.String())

	localClient, err := client.New(ctrl.GetConfigOrDie(), client.Options{})
	if err != nil {
		return errors.Wrap(err, "cannot create local client")
	}

	mgr, err := ctrl.NewManager(a.ClusterConfig, ctrl.Options{SyncPeriod: &period})
	if err != nil {
		return errors.Wrap(err, "cannot start remote cluster manager")
	}

	if err := crds.AddToScheme(mgr.GetScheme()); err != nil {
		return errors.Wrap(err, "Cannot add CustomResourceDefinition API to scheme")
	}

	if err := apiextensions.AddToScheme(mgr.GetScheme()); err != nil {
		return errors.Wrap(err, "Cannot add Crossplane apiextensions API to scheme")
	}

	if err := cluster.Setup(mgr, localClient, log); err != nil {
		return errors.Wrap(err, "Cannot setup cluster controllers")
	}

	return errors.Wrap(mgr.Start(ctrl.SetupSignalHandler()), "cannot start controller manager")
}
