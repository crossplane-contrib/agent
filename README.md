# Crosspane Agent

Note that this is an experimental project and is not meant to be used in
production deployments.

Crossplane Agent is installed to Kubernetes cluster that you would like to bind
with Crossplane instance. It makes all `CompositeResourceDefinition`s that are
published in that Crossplane instance available for consumption in your cluster.

Design document can be found here: https://github.com/crossplane/crossplane/blob/7d942fd/design/design-doc-agent.md

## Installation

### Set up a ServiceAccount in Crossplane Cluster

Before installing the agent, we need to create some `ServiceAccount`s with necessary
permissions in the cluster where Crossplane runs.

We can set up a `ServiceAccount` with permissions only in one namespace in
Crossplane cluster and use that in the agent cluster. For simplicity, we will
create a cluster-admin `ServiceAccount` in Crossplane cluster and give it to
agent as the default `ServiceAccount` to use for all namespaces.

Create the following in the Crossplane cluster:
```yaml
---
apiVersion: v1
kind: ServiceAccount
metadata:
  name: agent-one
  namespace: crossplane-system
---
apiVersion: rbac.authorization.k8s.io/v1
kind: ClusterRoleBinding
metadata:
  name: agent-admin
roleRef:
  apiGroup: rbac.authorization.k8s.io
  kind: ClusterRole
  name: cluster-admin
subjects:
- kind: ServiceAccount
  name: agent-one
  namespace: crossplane-system
```

Now we need to deliver the token of that `ServiceAccount` to agent. In order to
do that, we'll run a script that will generate a Kubeconfig to be used by the
agent. Run the following:

```bash
cluster/local/credentials.sh agent-one > /tmp/kubeconfig.yaml
```

If you are using `kind` clusters in your local, then you will need to change the
IP shown in the kubeconfig with the one that's exposed to your Docker network.
Run the following:
```bash
# you can change the name kind to whatever you use as name of the Crossplane cluster
kind get kubeconfig --internal --name kind | grep server:
```

Get that value and replace `clusters[0].server` value with it in
`/tmp/kubeconfig.yaml` where it's either `localhost` or `127.0.0.1`.

### Install Agent

Now we have our credentials to access Crossplane cluster, we can install the
agent and tell it to use that credentials.

Switch your `kubectl` context to the cluster where you'd like to install the agent.

Let's create a `Secret` that contains our Crossplane cluster credentials.
```bash
kubectl create secret -n default generic default-sa --from-file=kubeconfig=/tmp/kubeconfig.yaml
```

Docker images of the agent are not published yet, so we'll be building and
loading it into our cluster:
```bash
make build
# make sure you change build-918a30a8 with the correct value in the output of `make build`
docker tag build-918a30a8/agent-amd64 crossplane/agent:latest
# use the name you chose for your agent kind cluster
kind load docker-image --name agent crossplane/agent:latest
helm install agent --namespace default --set defaultCredentials.secretName=default-sa cluster/charts/agent
```

Congratulations! Crossplane agent is installed and running!

## Usage

After the installation, all `XRD`s and their generated `CRD`s should be
installed and you can check what's available to you by running `kubectl get
xrd` and use as if you are in Crossplane cluster.

## Missing Features

* There is a one-to-one namespace matching right now, i.e. if you create a claim
  in `a` namespace in agent cluster, then it goes to `a` namespace of Crossplane
  cluster. You will be able to change the target namespace by annotating your
  namespace in agent cluster.
* `ServiceAccount` per namespace. Right now, the same `ServiceAccount` is used
  for all operations.
* Publish images in each commit.
* Publish Helm charts.