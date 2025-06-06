//go:build e2e
// +build e2e

package nvidia

import (
	"context"
	"flag"
	"fmt"
	"math/rand"
	"testing"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	"gopkg.in/yaml.v2"

	"github.com/aws/eks-hybrid/test/e2e"
	"github.com/aws/eks-hybrid/test/e2e/suite"
)

var (
	filePath    string
	suiteConfig *suite.SuiteConfiguration
)

const numberOfNodes = 1

func init() {
	flag.StringVar(&filePath, "filepath", "", "Path to configuration")
}

func TestE2E(t *testing.T) {
	RegisterFailHandler(Fail)
	RunSpecs(t, "Nvidia Test Suite")
}

// This is to test nvidia GPU function in EKS hybrid nodes. We need to use nodes with nvidia GPUs for the test.
var _ = SynchronizedBeforeSuite(
	func(ctx context.Context) []byte {
		suiteConfig := suite.BeforeSuiteCredentialSetup(ctx, filePath)
		test := suite.BeforeVPCTest(ctx, &suiteConfig)
		credentialProviders := suite.AddClientsToCredentialProviders(suite.CredentialProviders(), test)
		osList := suite.OSProviderList(credentialProviders)

		// pick # of random OS/Version/Provider combinations for metricsServer tests worker nodes
		nodesToCreate := make([]suite.NodeCreate, 0, numberOfNodes)

		rand.Shuffle(len(osList), func(i, j int) {
			osList[i], osList[j] = osList[j], osList[i]
		})

		for i := range numberOfNodes {
			os := osList[i].OS
			provider := osList[i].Provider
			nodesToCreate = append(nodesToCreate, suite.NodeCreate{
				OS:           os,
				Provider:     provider,
				InstanceName: test.InstanceName("nvidia-test", os, provider),
				InstanceSize: e2e.Large,
				GpuInstance:  true,
				NodeName:     fmt.Sprintf("nvidia-node-%s-%s", provider.Name(), os.Name()),
			})
		}
		suite.CreateNodes(ctx, test, nodesToCreate)

		suiteJson, err := yaml.Marshal(suiteConfig)
		Expect(err).NotTo(HaveOccurred(), "suite config should be marshalled successfully")
		return suiteJson
	},
	// This function runs on all processes, and it receives the data from
	// the first process (a json serialized struct)
	// The only thing that we want to do here is unmarshal the data into
	// a struct that we can make accessible from the tests. We leave the rest
	// for the per tests setup code.
	func(ctx context.Context, data []byte) {
		suiteConfig = suite.BeforeSuiteCredentialUnmarshal(ctx, data)
	},
)

var _ = Describe("Hybrid Nodes", func() {
	When("using peered VPC", func() {
		var test *suite.NvidiaEc2Test

		BeforeEach(func(ctx context.Context) {
			test = &suite.NvidiaEc2Test{PeeredVPCTest: suite.BeforeVPCTest(ctx, suiteConfig)}
		})

		It("should have working NVIDIA GPU drivers", func(ctx context.Context) {
			test.Logger.Info("Checking NVIDIA drivers on node")
			devicePluginTest := test.NewNvidiaDevicePluginTest(numberOfNodes)
			err := devicePluginTest.WaitForNvidiaDriverReady(ctx)
			Expect(err).NotTo(HaveOccurred(), "NVIDIA drivers should be ready")
		})
	})
})
