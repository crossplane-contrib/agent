#!/bin/sh

set -eE

namespace=${NAMESPACE:-crossplane-system}

cluster_name=$(kubectl config get-contexts "$(kubectl config current-context)" | awk '{print $3}' | tail -n 1)
# your server name goes here
server=$(kubectl config view -o jsonpath="{.clusters[?(@.name == \"${cluster_name}\")].cluster.server}")
# the name of the secret containing the service account token goes here
secret_name=$(kubectl -n ${namespace} get sa "${1}" -o json | jq -r .secrets[].name)

ca=$(kubectl -n $namespace get secret/$secret_name -o jsonpath='{.data.ca\.crt}')
token=$(kubectl -n $namespace get secret/$secret_name -o jsonpath='{.data.token}' | base64 --decode)
namespace=$(kubectl -n $namespace get secret/$secret_name -o jsonpath='{.data.namespace}' | base64 --decode)

echo "apiVersion: v1
kind: Config
clusters:
- name: default-cluster
  cluster:
    certificate-authority-data: ${ca}
    server: ${server}
contexts:
- name: default-context
  context:
    cluster: default-cluster
    namespace: default
    user: default-user
current-context: default-context
users:
- name: default-user
  user:
    token: ${token}
"