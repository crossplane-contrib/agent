module github.com/crossplane/agent

go 1.13

replace github.com/crossplane/crossplane-runtime => github.com/muvaf/crossplane-runtime v0.0.0-20200811131745-38e9a848fa0c

require (
	github.com/crossplane/crossplane v0.13.0-rc.0.20200714032609-a4570446bc0f
	github.com/crossplane/crossplane-runtime v0.9.1-0.20200629170915-9a9a434f7321
	github.com/pkg/errors v0.8.1
	gopkg.in/alecthomas/kingpin.v2 v2.2.6
	k8s.io/api v0.18.2
	k8s.io/apiextensions-apiserver v0.18.2
	k8s.io/apimachinery v0.18.2
	k8s.io/client-go v0.18.2
	sigs.k8s.io/controller-runtime v0.6.0
)
