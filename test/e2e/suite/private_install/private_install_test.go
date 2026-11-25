//go:build e2e
// +build e2e

// Package private_install tests installing hybrid nodes from private artifacts on a node
// without internet access, following the private installation guide.
//
// This suite isn't part of the default E2E_SUITES and needs a cluster created with
// endpointPrivateAccess: true and the calico CNI (Cilium images come from public ECR,
// which has no VPC endpoint). See README.md in this directory.
package private_install

import (
	"context"
	"flag"
	"fmt"
	"io"
	"net/http"
	"strings"
	"testing"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	"gopkg.in/yaml.v2"

	"github.com/aws/eks-hybrid/internal/creds"
	"github.com/aws/eks-hybrid/test/e2e"
	"github.com/aws/eks-hybrid/test/e2e/constants"
	e2eOS "github.com/aws/eks-hybrid/test/e2e/os"
	"github.com/aws/eks-hybrid/test/e2e/privateinstall"
	"github.com/aws/eks-hybrid/test/e2e/suite"
)

// internetCheckURL is a public endpoint the node would normally download from.
const internetCheckURL = "https://hybrid-assets.eks.amazonaws.com/releases/latest/bin/linux/amd64/nodeadm"

var (
	filePath    string
	suiteConfig *suite.SuiteConfiguration
)

func init() {
	flag.StringVar(&filePath, "filepath", "", "Path to configuration")
}

func TestE2E(t *testing.T) {
	RegisterFailHandler(Fail)
	RunSpecs(t, "Nodeadm Private Install E2E Suite")
}

var _ = SynchronizedBeforeSuite(
	func(ctx context.Context) []byte {
		suiteConfig := suite.BeforeSuiteCredentialSetup(ctx, filePath)
		suiteJson, err := yaml.Marshal(suiteConfig)
		Expect(err).NotTo(HaveOccurred(), "suite config should be marshalled successfully")
		return suiteJson
	},
	func(ctx context.Context, data []byte) {
		suiteConfig = suite.BeforeSuiteCredentialUnmarshal(ctx, data)
	},
)

var _ = Describe("Hybrid Nodes private install", Ordered, Label("private-install"), func() {
	var (
		envConfig privateinstall.Config
		env       *privateinstall.Environment
	)

	BeforeAll(func(ctx context.Context) {
		test := suite.BeforeVPCTest(ctx, suiteConfig)
		if suiteConfig.TestConfig.Endpoint != "" {
			Skip("private install suite doesn't support a custom EKS endpoint, which has no VPC endpoint")
		}

		k8sVersion := test.Cluster.KubernetesVersion
		if test.OverrideNodeK8sVersion != "" {
			k8sVersion = test.OverrideNodeK8sVersion
		}

		envConfig = privateinstall.Config{
			EC2:               test.EC2Client,
			S3:                test.S3Client,
			EKS:               test.EKSClient,
			Logger:            test.Logger,
			Cluster:           test.Cluster,
			DNSSuffix:         test.DNSSuffix,
			KubernetesVersion: k8sVersion,
			NodeadmURLs:       test.NodeadmURLs(),
			EcrAccounts:       []string{test.EcrAccount},
			ArtifactsDir:      test.ArtifactsPath,
			// Sync from the custom manifest when one is configured for the run.
			ReleaseManifestURL: suiteConfig.TestConfig.ManifestURL,
		}

		var err error
		env, err = privateinstall.Setup(ctx, envConfig)
		DeferCleanup(func(ctx context.Context) {
			if test.SkipCleanup {
				test.Logger.Info("Skipping private install environment cleanup", "bucket", env.BucketName)
				return
			}
			Expect(privateinstall.Cleanup(ctx, envConfig, env)).To(Succeed(), "private install environment should be cleaned up")
		}, NodeTimeout(constants.DeferCleanupTimeout))
		Expect(err).NotTo(HaveOccurred(), "private install environment should be created")
	})

	It("denies reading artifacts from outside the VPC endpoint", func(ctx context.Context) {
		req, err := http.NewRequestWithContext(ctx, http.MethodGet, env.ManifestURL, nil)
		Expect(err).NotTo(HaveOccurred())
		resp, err := http.DefaultClient.Do(req)
		Expect(err).NotTo(HaveOccurred())
		defer resp.Body.Close()
		_, _ = io.Copy(io.Discard, resp.Body)
		Expect(resp.StatusCode).To(Equal(http.StatusForbidden), "manifest should only be readable through the VPC endpoint")
	})

	DescribeTable("Joining a node without internet access",
		func(ctx context.Context, providerName creds.CredentialProvider) {
			test := suite.BeforeVPCTest(ctx, suiteConfig)
			providers := suite.AddClientsToCredentialProviders(suite.CredentialProviders(), test)
			var provider e2e.NodeadmCredentialsProvider
			for _, p := range providers {
				if p.Name() == providerName {
					provider = p
				}
			}
			Expect(provider).NotTo(BeNil(), "credential provider %s should exist", providerName)

			nodeOS := e2eOS.NewAmazonLinux2023AMD()
			k8sVersion := envConfig.KubernetesVersion
			instanceName := test.InstanceName("private", nodeOS.Name(), string(provider.Name()))
			nodeName := "private-install-node-" + string(provider.Name()) + "-" + nodeOS.Name()

			testNode := test.NewTestNode(ctx, instanceName, nodeName, k8sVersion, nodeOS, provider, e2e.Large, e2e.CPUInstance,
				suite.WithPrivateInstall(env.NodeSpec()))
			Expect(testNode.Start(ctx)).To(Succeed(), "node should start successfully")
			Expect(testNode.WaitForJoin(ctx)).To(Succeed(), "node should join successfully")
			Expect(testNode.Verify(ctx)).To(Succeed(), "node should be fully functional")

			instance := testNode.PeeredInstance()
			peeredNode := test.NewPeeredNode(test.Logger)
			testNode.It("can't reach the internet", func() {
				output, err := peeredNode.RemoteCommandRunner.Run(ctx, instance.IP, []string{
					fmt.Sprintf("if curl -sS --max-time 15 -o /dev/null %s; then echo INTERNET_REACHABLE; else echo INTERNET_BLOCKED; fi", internetCheckURL),
				})
				Expect(err).NotTo(HaveOccurred(), "command should run on the node")
				Expect(strings.Contains(output.StandardOutput, "INTERNET_BLOCKED")).To(BeTrue(),
					"node should not be able to reach %s, output: %s", internetCheckURL, output.StandardOutput)
			})

			if test.SkipCleanup {
				test.Logger.Info("Skipping nodeadm uninstall from the hybrid node...")
				return
			}

			cleanNode := test.NewCleanNode(
				provider,
				testNode.PeeredNode.NodeInfrastructureCleaner(*instance),
				instance.Name,
				instance.IP,
			)
			Expect(cleanNode.Run(ctx)).To(Succeed(), "node should have been reset successfully")
		},
		Entry("With OS al23-amd64 and with Credential Provider ssm", creds.SsmCredentialProvider, Label("al23-amd64", "ssm")),
		Entry("With OS al23-amd64 and with Credential Provider iam-ra", creds.IamRolesAnywhereCredentialProvider, Label("al23-amd64", "iam-ra")),
	)
})
