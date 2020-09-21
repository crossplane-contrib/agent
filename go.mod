module github.com/crossplane/agent

go 1.13

require (
	github.com/crossplane/crossplane v0.13.0-rc.0.20200828222536-fe3c37122ee6
	github.com/crossplane/crossplane-runtime v0.9.1-0.20200831142237-1576699ee9ac
	github.com/google/go-cmp v0.4.0
	github.com/pkg/errors v0.9.1
	gopkg.in/alecthomas/kingpin.v2 v2.2.6
	k8s.io/api v0.18.6
	k8s.io/apiextensions-apiserver v0.18.6
	k8s.io/apimachinery v0.18.6
	k8s.io/client-go v0.18.6
	sigs.k8s.io/controller-runtime v0.6.2
)
