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

	"gopkg.in/alecthomas/kingpin.v2"
	"k8s.io/client-go/tools/clientcmd"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/log/zap"

	"github.com/crossplane/crossplane-runtime/pkg/logging"

	"github.com/crossplane/agent/cmd/agent/local"
	"github.com/crossplane/agent/cmd/agent/remote"
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
	mode := s.Flag("mode", "The mode of operation to decide whether you would like to run the controllers that watch the local cluster or the remote cluster.").Enum("local", "remote")

	kingpin.MustParse(app.Parse(os.Args[1:]))
	zl := zap.New(zap.UseDevMode(*debug))
	if *debug {
		// The controller-runtime runs with a no-op logger by default. It is
		// *very* verbose even at info level, so we only provide it a real
		// logger when we're running in debug mode.
		ctrl.SetLogger(zl)
	}
	defaultConfig, err := clientcmd.BuildConfigFromFlags("", *dsa)
	if err != nil {
		kingpin.FatalUsage("could not parse default kubeconfig %s", *dsa)
	}
	clusterConfig, err := clientcmd.BuildConfigFromFlags("", *csa)
	if err != nil {
		kingpin.FatalUsage("could not parse cluster kubeconfig %s", *csa)
	}
	duration, _ := time.ParseDuration("1h")
	switch *mode {
	case "local":
		agent := &local.Agent{
			ClusterConfig: clusterConfig,
			DefaultConfig: defaultConfig,
		}
		kingpin.FatalIfError(agent.Run(logging.NewLogrLogger(zl.WithName("crossplane-agent")), duration), "cannot run agent in local mode")
	case "remote":
		agent := &remote.Agent{
			ClusterConfig: clusterConfig,
		}
		kingpin.FatalIfError(agent.Run(logging.NewLogrLogger(zl.WithName("crossplane-agent")), duration), "cannot run agent in remote mode")
	}
}
