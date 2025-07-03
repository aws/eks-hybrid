//go:build e2e
// +build e2e

package addons

import (
	"context"
	"flag"
	"fmt"
	"math/rand"
	"testing"

	. "github.com/onsi/ginkgo/v2"
	"github.com/onsi/ginkgo/v2/types"
	. "github.com/onsi/gomega"
	"gopkg.in/yaml.v2"
	"k8s.io/apimachinery/pkg/labels"
	"k8s.io/apimachinery/pkg/selection"

	"github.com/aws/eks-hybrid/test/e2e"
	"github.com/aws/eks-hybrid/test/e2e/ssm"
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
	RunSpecs(t, "Addon Smoke Test Suite")
}

var _ = SynchronizedBeforeSuite(
	func(ctx context.Context) []byte {
		suiteConfig := suite.BeforeSuiteCredentialSetup(ctx, filePath)
		test := suite.BeforeVPCTest(ctx, &suiteConfig)

		credentialProviders := suite.AddClientsToCredentialProviders(suite.CredentialProviders(), test)
		osList := suite.OSProviderList(credentialProviders)
		bottlerocketList := suite.BottlerocketOSProviderList()

		// pick 4 random OS/Version/Provider combination for metrics server, NMA and Nvidia tests
		// 2 AL2023/RHEL/Ubuntu + 2 Bottlerocket
		nodesToCreate := make([]suite.NodeCreate, 0, numberOfNodes*4)

		// Shuffle once and use different indices to avoid duplicates
		rand.Shuffle(len(osList), func(i, j int) {
			osList[i], osList[j] = osList[j], osList[i]
		})
		rand.Shuffle(len(bottlerocketList), func(i, j int) {
			bottlerocketList[i], bottlerocketList[j] = bottlerocketList[j], bottlerocketList[i]
		})

		// Create regular OS node
		for i := range numberOfNodes {
			os := osList[i].OS
			provider := osList[i].Provider
			test.Logger.Info(fmt.Sprintf("Creating Node %d with OS %s and Credential Provider %s", i+1, os.Name(), string(provider.Name())))
			nodesToCreate = append(nodesToCreate, suite.NodeCreate{
				OS:           os,
				Provider:     provider,
				InstanceName: test.InstanceName("addon-smoke-test", os.Name(), string(provider.Name())),
				InstanceSize: e2e.Large,
				ComputeType:  e2e.CPUInstance,
				NodeName:     fmt.Sprintf("addon-test-node-%s-%s", provider.Name(), os.Name()),
			})
		}

		// Create Bottlerocket node
		for i := range numberOfNodes {
			os := bottlerocketList[i].OS
			provider := bottlerocketList[i].Provider
			test.Logger.Info(fmt.Sprintf("Creating Bottlerocket Node %d with OS %s and Credential Provider %s", i+1, os.Name(), string(provider.Name())))
			nodesToCreate = append(nodesToCreate, suite.NodeCreate{
				OS:           os,
				Provider:     provider,
				InstanceName: test.InstanceName("addon-smoke-test", os.Name(), string(provider.Name())),
				InstanceSize: e2e.Large,
				ComputeType:  e2e.CPUInstance,
				NodeName:     fmt.Sprintf("addon-test-node-%s-%s", provider.Name(), os.Name()),
			})
		}

		// Create regular OS GPU node for NVIDIA tests - use different index to avoid duplicates
		gpuOSIndex := numberOfNodes % len(osList)
		if gpuOSIndex == 0 && len(osList) > 1 {
			gpuOSIndex = 1 // Ensure we don't use the same as the first regular node
		}
		for i := range numberOfNodes {
			idx := (gpuOSIndex + i) % len(osList)
			os := osList[idx].OS
			provider := osList[idx].Provider
			test.Logger.Info(fmt.Sprintf("Creating GPU Node %d with OS %s and Credential Provider %s", i+1, os.Name(), string(provider.Name())))
			gpuNodeName := fmt.Sprintf("addon-nvidia-node-%s-%s", provider.Name(), os.Name())
			nodesToCreate = append(nodesToCreate, suite.NodeCreate{
				OS:           os,
				Provider:     provider,
				InstanceName: test.InstanceName("addon-nvidia-test", os.Name(), string(provider.Name())),
				InstanceSize: e2e.Large,
				ComputeType:  e2e.GPUInstance,
				NodeName:     gpuNodeName,
			})
		}

		// Create Bottlerocket GPU node for NVIDIA tests - use different index to avoid duplicates
		bottlerocketGPUIndex := numberOfNodes % len(bottlerocketList)
		if bottlerocketGPUIndex == 0 && len(bottlerocketList) > 1 {
			bottlerocketGPUIndex = 1 // Ensure we don't use the same as the first Bottlerocket node
		}
		for i := range numberOfNodes {
			idx := (bottlerocketGPUIndex + i) % len(bottlerocketList)
			os := bottlerocketList[idx].OS
			provider := bottlerocketList[idx].Provider
			test.Logger.Info(fmt.Sprintf("Creating Bottlerocket GPU Node %d with OS %s and Credential Provider %s", i+1, os.Name(), string(provider.Name())))
			bottlerocketGPUNodeName := fmt.Sprintf("addon-nvidia-bottlerocket-node-%s-%s", provider.Name(), os.Name())
			nodesToCreate = append(nodesToCreate, suite.NodeCreate{
				OS:           os,
				Provider:     provider,
				InstanceName: test.InstanceName("addon-nvidia-bottlerocket-test", os.Name(), string(provider.Name())),
				InstanceSize: e2e.Large,
				ComputeType:  e2e.GPUInstance,
				NodeName:     bottlerocketGPUNodeName,
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
		var addonEc2Test *suite.AddonEc2Test
		credentialProviders := suite.CredentialProviders()

		BeforeEach(func(ctx context.Context) {
			addonEc2Test = &suite.AddonEc2Test{PeeredVPCTest: suite.BeforeVPCTest(ctx, suiteConfig)}
			credentialProviders = suite.AddClientsToCredentialProviders(credentialProviders, addonEc2Test.PeeredVPCTest)
		})

		When("using ec2 instance as hybrid nodes", func() {
			It("runs node monitoring agent tests", func(ctx context.Context) {
				nodeMonitoringAgent := addonEc2Test.NewNodeMonitoringAgentTest()

				DeferCleanup(func(ctx context.Context) {
					Expect(nodeMonitoringAgent.Delete(ctx)).To(Succeed(), "should cleanup node monitoring agent successfully")
				})

				Expect(nodeMonitoringAgent.Create(ctx)).To(
					Succeed(), "node monitoring agent should have created successfully",
				)

				DeferCleanup(func(ctx context.Context) {
					// only print logs after addon successfully created
					report := CurrentSpecReport()
					if report.State.Is(types.SpecStateFailed) {
						err := nodeMonitoringAgent.PrintLogs(ctx)
						if err != nil {
							// continue cleanup even if logs collection fails
							GinkgoWriter.Printf("Failed to get node monitoring agent logs: %v\n", err)
						}
					}
				})

				Expect(nodeMonitoringAgent.Validate(ctx)).To(
					Succeed(), "node monitoring agent should have been validated successfully",
				)
			}, Label("node-monitoring-agent"))

			It("runs kube state metrics tests", func(ctx context.Context) {
				kubeStateMetrics := addonEc2Test.NewKubeStateMetricsTest()

				DeferCleanup(func(ctx context.Context) {
					Expect(kubeStateMetrics.Delete(ctx)).To(Succeed(), "should cleanup kube state metrics successfully")
				})

				Expect(kubeStateMetrics.Create(ctx)).To(
					Succeed(), "kube state metrics should have created successfully",
				)

				DeferCleanup(func(ctx context.Context) {
					// only print logs after addon successfully created
					report := CurrentSpecReport()
					if report.State.Is(types.SpecStateFailed) {
						err := kubeStateMetrics.PrintLogs(ctx)
						if err != nil {
							// continue cleanup even if logs collection fails
							GinkgoWriter.Printf("Failed to get kube state metrics logs: %v\n", err)
						}
					}
				})

				Expect(kubeStateMetrics.Validate(ctx)).To(
					Succeed(), "kube state metrics should have been validated successfully",
				)
			}, Label("kube-state-metrics"))

			It("runs metrics server tests", func(ctx context.Context) {
				metricsServer := addonEc2Test.NewMetricsServerTest()

				DeferCleanup(func(ctx context.Context) {
					Expect(metricsServer.Delete(ctx)).To(Succeed(), "should cleanup metrics server successfully")
				})

				Expect(metricsServer.Create(ctx)).To(
					Succeed(), "metrics server should have created successfully",
				)

				DeferCleanup(func(ctx context.Context) {
					// only print logs after addon successfully created
					report := CurrentSpecReport()
					if report.State.Is(types.SpecStateFailed) {
						err := metricsServer.PrintLogs(ctx)
						if err != nil {
							// continue cleanup even if logs collection fails
							GinkgoWriter.Printf("Failed to get metrics server logs: %v\n", err)
						}
					}
				})

				Expect(metricsServer.Validate(ctx)).To(
					Succeed(), "metrics server should have been validated successfully",
				)
			}, Label("metrics-server"))

			It("runs prometheus node exporter tests", func(ctx context.Context) {
				prometheusNodeExporter := addonEc2Test.NewPrometheusNodeExporterTest()

				DeferCleanup(func(ctx context.Context) {
					Expect(prometheusNodeExporter.Delete(ctx)).To(Succeed(), "should cleanup prometheus node exporter successfully")
				})

				Expect(prometheusNodeExporter.Create(ctx)).To(
					Succeed(), "prometheus node exporter should have created successfully",
				)

				DeferCleanup(func(ctx context.Context) {
					// only print logs after addon successfully created
					report := CurrentSpecReport()
					if report.State.Is(types.SpecStateFailed) {
						err := prometheusNodeExporter.PrintLogs(ctx)
						if err != nil {
							// continue cleanup even if logs collection fails
							GinkgoWriter.Printf("Failed to get prometheus node exporter logs: %v\n", err)
						}
					}
				})

				Expect(prometheusNodeExporter.Validate(ctx)).To(
					Succeed(), "prometheus node exporter should have been validated successfully",
				)
			}, Label("prometheus-node-exporter"))

			It("runs nvidia device plugin tests", func(ctx context.Context) {
				// Use the suite configuration value set during node creation in SynchronizedBeforeSuite
				Expect(suiteConfig.RegularGPUNodeName).NotTo(BeEmpty(), "regular GPU node name should be set")

				// wait for nvidia drivers to be installed
				addonEc2Test.Logger.Info("Checking NVIDIA drivers on pre-created GPU node", "nodeName", suiteConfig.RegularGPUNodeName)
				devicePluginTest := addonEc2Test.NewNvidiaDevicePluginTest(suiteConfig.RegularGPUNodeName)
				Expect(devicePluginTest.WaitForNvidiaDriverReady(ctx)).NotTo(HaveOccurred(), "NVIDIA drivers should be ready")
			}, Label("nvidia-device-plugin"))

			It("runs cert manager tests", func(ctx context.Context) {
				certManager := addonEc2Test.NewCertManagerTest()

				DeferCleanup(func(ctx context.Context) {
					Expect(certManager.Delete(ctx)).To(Succeed(), "should cleanup cert manager successfully")
				})

				Expect(certManager.Create(ctx)).To(
					Succeed(), "cert manager should have created successfully",
				)

				DeferCleanup(func(ctx context.Context) {
					// only print logs after addon successfully created
					report := CurrentSpecReport()
					if report.State.Is(types.SpecStateFailed) {
						err := certManager.PrintLogs(ctx)
						if err != nil {
							// continue cleanup even if logs collection fails
							GinkgoWriter.Printf("Failed to get cert manager logs: %v\n", err)
						}
					}
				})

				Expect(certManager.Validate(ctx)).To(
					Succeed(), "cert manager should have been validated successfully",
				)
			}, Label("cert-manager"))
		})

		When("using Bottlerocket ec2 instance as hybrid nodes", func() {
			It("runs node monitoring agent tests on Bottlerocket", func(ctx context.Context) {
				// Create the node monitoring agent test using the standard method
				nodeMonitoringAgent := addonEc2Test.NewNodeMonitoringAgentTest()

				// Override the command runner and node filter for Bottlerocket, similar to nodeadm_test.go pattern
				bottlerocketCommandRunner := ssm.NewBottlerocketSSHOnSSMCommandRunner(addonEc2Test.SSMClient, addonEc2Test.JumpboxInstanceId, addonEc2Test.Logger)
				nodeMonitoringAgent.CommandRunner = bottlerocketCommandRunner
				labelReq, _ := labels.NewRequirement("os.bottlerocket.aws/version", selection.Exists, []string{})
				nodeMonitoringAgent.NodeFilter = labels.NewSelector().Add(*labelReq)

				DeferCleanup(func(ctx context.Context) {
					Expect(nodeMonitoringAgent.Delete(ctx)).To(Succeed(), "should cleanup node monitoring agent successfully")
				})

				Expect(nodeMonitoringAgent.Create(ctx)).To(
					Succeed(), "node monitoring agent should have created successfully",
				)

				DeferCleanup(func(ctx context.Context) {
					// only print logs after addon successfully created
					report := CurrentSpecReport()
					if report.State.Is(types.SpecStateFailed) {
						err := nodeMonitoringAgent.PrintLogs(ctx)
						if err != nil {
							// continue cleanup even if logs collection fails
							GinkgoWriter.Printf("Failed to get node monitoring agent logs: %v\n", err)
						}
					}
				})

				Expect(nodeMonitoringAgent.Validate(ctx)).To(
					Succeed(), "node monitoring agent should have been validated successfully",
				)
			}, Label("node-monitoring-agent", "bottlerocket"))

			It("runs metrics server tests on Bottlerocket", func(ctx context.Context) {
				metricsServer := addonEc2Test.NewMetricsServerTest()

				DeferCleanup(func(ctx context.Context) {
					Expect(metricsServer.Delete(ctx)).To(Succeed(), "should cleanup metrics server successfully")
				})

				Expect(metricsServer.Create(ctx)).To(
					Succeed(), "metrics server should have created successfully",
				)

				DeferCleanup(func(ctx context.Context) {
					// only print logs after addon successfully created
					report := CurrentSpecReport()
					if report.State.Is(types.SpecStateFailed) {
						err := metricsServer.PrintLogs(ctx)
						if err != nil {
							// continue cleanup even if logs collection fails
							GinkgoWriter.Printf("Failed to get metrics server logs: %v\n", err)
						}
					}
				})

				Expect(metricsServer.Validate(ctx)).To(
					Succeed(), "metrics server should have been validated successfully",
				)
			}, Label("metrics-server", "bottlerocket"))

			It("runs kube state metrics tests on Bottlerocket", func(ctx context.Context) {
				kubeStateMetrics := addonEc2Test.NewKubeStateMetricsTest()

				DeferCleanup(func(ctx context.Context) {
					Expect(kubeStateMetrics.Delete(ctx)).To(Succeed(), "should cleanup kube state metrics successfully")
				})

				Expect(kubeStateMetrics.Create(ctx)).To(
					Succeed(), "kube state metrics should have created successfully",
				)

				DeferCleanup(func(ctx context.Context) {
					// only print logs after addon successfully created
					report := CurrentSpecReport()
					if report.State.Is(types.SpecStateFailed) {
						err := kubeStateMetrics.PrintLogs(ctx)
						if err != nil {
							// continue cleanup even if logs collection fails
							GinkgoWriter.Printf("Failed to get kube state metrics logs: %v\n", err)
						}
					}
				})

				Expect(kubeStateMetrics.Validate(ctx)).To(
					Succeed(), "kube state metrics should have been validated successfully",
				)
			}, Label("kube-state-metrics", "bottlerocket"))

			It("runs prometheus node exporter tests on Bottlerocket", func(ctx context.Context) {
				prometheusNodeExporter := addonEc2Test.NewPrometheusNodeExporterTest()

				DeferCleanup(func(ctx context.Context) {
					Expect(prometheusNodeExporter.Delete(ctx)).To(Succeed(), "should cleanup prometheus node exporter successfully")
				})

				Expect(prometheusNodeExporter.Create(ctx)).To(
					Succeed(), "prometheus node exporter should have created successfully",
				)

				DeferCleanup(func(ctx context.Context) {
					// only print logs after addon successfully created
					report := CurrentSpecReport()
					if report.State.Is(types.SpecStateFailed) {
						err := prometheusNodeExporter.PrintLogs(ctx)
						if err != nil {
							// continue cleanup even if logs collection fails
							GinkgoWriter.Printf("Failed to get prometheus node exporter logs: %v\n", err)
						}
					}
				})

				Expect(prometheusNodeExporter.Validate(ctx)).To(
					Succeed(), "prometheus node exporter should have been validated successfully",
				)
			}, Label("prometheus-node-exporter", "bottlerocket"))

			It("runs nvidia device plugin tests on Bottlerocket", func(ctx context.Context) {
				// Use the suite configuration value set during node creation in SynchronizedBeforeSuite
				Expect(suiteConfig.BottlerocketGPUNodeName).NotTo(BeEmpty(), "Bottlerocket GPU node name should be set")

				// wait for nvidia drivers to be installed
				addonEc2Test.Logger.Info("Checking NVIDIA drivers on pre-created Bottlerocket GPU node", "nodeName", suiteConfig.BottlerocketGPUNodeName)
				devicePluginTest := addonEc2Test.NewNvidiaDevicePluginTest(suiteConfig.BottlerocketGPUNodeName)

				// Override the command runner for Bottlerocket, similar to nodeadm_test.go pattern
				bottlerocketCommandRunner := ssm.NewBottlerocketSSHOnSSMCommandRunner(addonEc2Test.SSMClient, addonEc2Test.JumpboxInstanceId, addonEc2Test.Logger)
				devicePluginTest.CommandRunner = bottlerocketCommandRunner

				Expect(devicePluginTest.WaitForNvidiaDriverReady(ctx)).NotTo(HaveOccurred(), "NVIDIA drivers should be ready")
			}, Label("nvidia-device-plugin", "bottlerocket"))

			It("runs cert manager tests on Bottlerocket", func(ctx context.Context) {
				certManager := addonEc2Test.NewCertManagerTest()

				DeferCleanup(func(ctx context.Context) {
					Expect(certManager.Delete(ctx)).To(Succeed(), "should cleanup cert manager successfully")
				})

				Expect(certManager.Create(ctx)).To(
					Succeed(), "cert manager should have created successfully",
				)

				DeferCleanup(func(ctx context.Context) {
					// only print logs after addon successfully created
					report := CurrentSpecReport()
					if report.State.Is(types.SpecStateFailed) {
						err := certManager.PrintLogs(ctx)
						if err != nil {
							// continue cleanup even if logs collection fails
							GinkgoWriter.Printf("Failed to get cert manager logs: %v\n", err)
						}
					}
				})

				Expect(certManager.Validate(ctx)).To(
					Succeed(), "cert manager should have been validated successfully",
				)
			}, Label("cert-manager", "bottlerocket"))
		})
	})
})
