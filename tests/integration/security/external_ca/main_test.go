//go:build integ
// +build integ

//  Copyright Istio Authors
//
//  Licensed under the Apache License, Version 2.0 (the "License");
//  you may not use this file except in compliance with the License.
//  You may obtain a copy of the License at
//
//      http://www.apache.org/licenses/LICENSE-2.0
//
//  Unless required by applicable law or agreed to in writing, software
//  distributed under the License is distributed on an "AS IS" BASIS,
//  WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
//  See the License for the specific language governing permissions and
//  limitations under the License.

package externalca

import (
	"testing"

	csrctrl "istio.io/istio/pkg/test/csrctrl/controllers"
	"istio.io/istio/pkg/test/framework"
	"istio.io/istio/pkg/test/framework/components/echo"
	"istio.io/istio/pkg/test/framework/components/echo/echoboot"
	"istio.io/istio/pkg/test/framework/components/istio"
	"istio.io/istio/pkg/test/framework/components/namespace"
	"istio.io/istio/pkg/test/framework/label"
	"istio.io/istio/pkg/test/framework/resource"
	"istio.io/istio/tests/integration/security/util"
)

const (
	ASvc = "a"
	BSvc = "b"
)

type EchoDeployments struct {
	Namespace namespace.Instance
	// workloads for TestSecureNaming
	A, B echo.Instances
}

var (
	inst     istio.Instance
	apps     = &EchoDeployments{}
	stopChan = make(chan struct{})
)

func SetupApps(ctx resource.Context, apps *EchoDeployments) error {
	var err error
	apps.Namespace, err = namespace.New(ctx, namespace.Config{
		Prefix: "test-ns",
		Inject: true,
	})
	if err != nil {
		return err
	}

	builder := echoboot.NewBuilder(ctx)
	builder.
		WithClusters(ctx.Clusters()...).
		WithConfig(util.EchoConfig(ASvc, apps.Namespace, false, nil)).
		WithConfig(util.EchoConfig(BSvc, apps.Namespace, false, nil))

	echos, err := builder.Build()
	if err != nil {
		return err
	}
	apps.A = echos.Match(echo.Service(ASvc))
	apps.B = echos.Match(echo.Service(BSvc))
	return nil
}

func TestMain(m *testing.M) {
	// Integration test for testing interoperability with external CA's that are integrated with K8s CSR API
	// Refer to https://kubernetes.io/docs/reference/access-authn-authz/certificate-signing-requests/
	framework.NewSuite(m).
		Label(label.CustomSetup).
		RequireMinVersion(19).
		RequireSingleCluster().
		RequireMultiPrimary().
		Setup(istio.Setup(&inst, setupConfig)).
		Setup(func(ctx resource.Context) error {
			return SetupApps(ctx, apps)
		}).
		Run()
	stopChan <- struct{}{}
	close(stopChan)
}

func setupConfig(ctx resource.Context, cfg *istio.Config) {
	go csrctrl.RunCSRController("clusterissuers.istio.io/signer1", ctx.Clusters()[0].RESTConfig(), stopChan)
	if cfg == nil {
		return
	}
	cfg.ControlPlaneValues = `
meshConfig:
  defaultConfig:
    proxyMetadata:
      ISTIO_META_CERT_SIGNER: signer1
components:
  pilot:
    k8s:
      env:
      - name: CERT_SIGNER_DOMAIN
        value: clusterissuers.istio.io
      - name: EXTERNAL_CA
        value: ISTIOD_RA_KUBERNETES_API
      overlays:
        # Amend ClusterRole to add permission for istiod to approve certificate signing by custom signer
        - kind: ClusterRole
          name: istiod-clusterrole-istio-system
          patches:
            - path: rules[-1]
              value: |
                apiGroups:
                - certificates.k8s.io
                resourceNames:
                - clusterissuers.istio.io/*
                resources:
                - signers
                verbs:
                - approve
values:
  meshConfig:
    trustDomainAliases: [some-other, trust-domain-foo]
`
	cfg.DeployEastWestGW = false
}
