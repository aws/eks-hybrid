//go:build e2e
// +build e2e

package addons

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"math/rand/v2"
	"strings"
	"testing"

	. "github.com/onsi/ginkgo/v2"
	"github.com/onsi/ginkgo/v2/types"
	. "github.com/onsi/gomega"
	"gopkg.in/yaml.v2"
	"k8s.io/apimachinery/pkg/labels"
	"k8s.io/apimachinery/pkg/selection"

	"github.com/aws/eks-hybrid/test/e2e"
	"github.com/aws/eks-hybrid/test/e2e/addon"
	"github.com/aws/eks-hybrid/test/e2e/ssm"
	"github.com/aws/eks-hybrid/test/e2e/suite"
)

const (
	standardLinuxGPUNodeName = "eks-hybrid-addons-standard-linux-gpu"
	bottlerocketGPUNodeName  = "eks-hybrid-addons-bottlerocket-gpu"
)

var (
	filePath        string
	suiteConfig     *suite.SuiteConfiguration
	gpuNodesCreated bool
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
		osList := suite.OSProviderList(credentialProviders, test.Cluster.Region)
		bottlerocketList := suite.BottlerocketOSProviderList(credentialProviders)
		nodesToCreate := make([]suite.NodeCreate, 0, numberOfNodes*4)

		// pick 4 random OS/Version/Provider combination for metrics server, NMA and Nvidia tests
		// 2 AL2023/RHEL/Ubuntu + 2 Bottlerocket
		standardLinuxCPUCombination, standardLinuxGPUCombination := getRandomElements(osList)
		bottlerocketCPUCombination, bottlerocketGPUCombination := getRandomElements(bottlerocketList)

		// Create Standard Linux CPU node
		for i := range numberOfNodes {
			os := standardLinuxCPUCombination.OS
			provider := standardLinuxCPUCombination.Provider
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

		// Create Bottlerocket CPU node
		for i := range numberOfNodes {
			os := bottlerocketCPUCombination.OS
			provider := bottlerocketCPUCombination.Provider
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

		// Temporarily skip testing in China since default limits are 0.
		if !strings.HasPrefix(suiteConfig.TestConfig.ClusterRegion, "cn-") {
			// Create Standard Linux GPU node
			for i := range numberOfNodes {
				os := standardLinuxGPUCombination.OS
				provider := standardLinuxGPUCombination.Provider
				test.Logger.Info(fmt.Sprintf("Creating GPU Node %d with OS %s and Credential Provider %s", i+1, os.Name(), string(provider.Name())))
				nodesToCreate = append(nodesToCreate, suite.NodeCreate{
					OS:           os,
					Provider:     provider,
					InstanceName: test.InstanceName("addon-nvidia-test", os.Name(), string(provider.Name())),
					InstanceSize: e2e.Large,
					ComputeType:  e2e.GPUInstance,
					NodeName:     standardLinuxGPUNodeName,
				})
			}

			// Create Bottlerocket GPU node
			for i := range numberOfNodes {
				os := bottlerocketGPUCombination.OS
				provider := bottlerocketGPUCombination.Provider
				test.Logger.Info(fmt.Sprintf("Creating Bottlerocket GPU Node %d with OS %s and Credential Provider %s", i+1, os.Name(), string(provider.Name())))
				nodesToCreate = append(nodesToCreate, suite.NodeCreate{
					OS:           os,
					Provider:     provider,
					InstanceName: test.InstanceName("addon-nvidia-bottlerocket-test", os.Name(), string(provider.Name())),
					InstanceSize: e2e.Large,
					ComputeType:  e2e.GPUInstance,
					NodeName:     bottlerocketGPUNodeName,
				})
			}
			gpuNodesCreated = true
		} else {
			test.Logger.Info("Skipping GPU node creation: G-family instances require service quotas limit increase")
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

		BeforeEach(func(ctx context.Context) {
			addonEc2Test = &suite.AddonEc2Test{PeeredVPCTest: suite.BeforeVPCTest(ctx, suiteConfig)}
		})

		When("using ec2 instance as hybrid nodes", func() {
			Context("runs node monitoring agent tests", Ordered, func() {
				It("uses regular OS (Ubuntu, AL, RHEL)", func(ctx context.Context) {
					nodeMonitoringAgent := addonEc2Test.NewNodeMonitoringAgentTest()

					DeferCleanup(func(ctx context.Context) {
						Expect(nodeMonitoringAgent.Delete(ctx)).To(Succeed(), "should cleanup node monitoring agent successfully")
					})

					skipIfAddonNotAvailable(nodeMonitoringAgent.Create(ctx), nodeMonitoringAgent.AddonName())

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
				}, Label("regular-os"))

				It("uses Bottlerocket OS", func(ctx context.Context) {
					// Create the node monitoring agent test using the standard method
					nodeMonitoringAgent := addonEc2Test.NewNodeMonitoringAgentTest()
					nodeMonitoringAgent.Command = "echo 'watchdog: BUG: soft lockup - CPU#6 stuck for 23s! [VM Thread:4054]' | sudo /usr/sbin/chroot /.bottlerocket/rootfs/ tee -a /dev/kmsg"
					nodeMonitoringAgent.CommandRunner = ssm.NewBottlerocketSSHOnSSMCommandRunner(addonEc2Test.SSMClient, addonEc2Test.JumpboxInstanceId, addonEc2Test.Logger)
					labelReq, _ := labels.NewRequirement("os.bottlerocket.aws/version", selection.Exists, []string{})
					nodeMonitoringAgent.NodeFilter = labels.NewSelector().Add(*labelReq)

					DeferCleanup(func(ctx context.Context) {
						Expect(nodeMonitoringAgent.Delete(ctx)).To(Succeed(), "should cleanup node monitoring agent successfully")
					})

					skipIfAddonNotAvailable(nodeMonitoringAgent.Create(ctx), nodeMonitoringAgent.AddonName())

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
				}, Label("bottlerocket"))
			}, Label("node-monitoring-agent"))

			Context("runs kube state metrics tests", func() {
				It("uses all OS", func(ctx context.Context) {
					kubeStateMetrics := addonEc2Test.NewKubeStateMetricsTest()

					DeferCleanup(func(ctx context.Context) {
						Expect(kubeStateMetrics.Delete(ctx)).To(Succeed(), "should cleanup kube state metrics successfully")
					})

					skipIfAddonNotAvailable(kubeStateMetrics.Create(ctx), kubeStateMetrics.AddonName())

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
				})
			}, Label("kube-state-metrics"))

			Context("runs metrics server tests", func() {
				It("uses regular OS (Ubuntu, AL, RHEL)", func(ctx context.Context) {
					metricsServer := addonEc2Test.NewMetricsServerTest()

					DeferCleanup(func(ctx context.Context) {
						Expect(metricsServer.Delete(ctx)).To(Succeed(), "should cleanup metrics server successfully")
					})

					skipIfAddonNotAvailable(metricsServer.Create(ctx), metricsServer.AddonName())

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
				})
			}, Label("metrics-server"))

			Context("runs prometheus node exporter tests", func() {
				It("uses regular OS (Ubuntu, AL, RHEL)", func(ctx context.Context) {
					prometheusNodeExporter := addonEc2Test.NewPrometheusNodeExporterTest()

					DeferCleanup(func(ctx context.Context) {
						Expect(prometheusNodeExporter.Delete(ctx)).To(Succeed(), "should cleanup prometheus node exporter successfully")
					})

					skipIfAddonNotAvailable(prometheusNodeExporter.Create(ctx), prometheusNodeExporter.AddonName())

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
				})
			}, Label("prometheus-node-exporter"))

			Context("runs nvidia device plugin tests", Ordered, func() {
				It("uses regular OS (Ubuntu, AL, RHEL)", func(ctx context.Context) {
					if !gpuNodesCreated {
						Skip("GPU nodes were not created, skipping nvidia device plugin test")
					}
					// wait for nvidia drivers to be installed
					addonEc2Test.Logger.Info("Checking NVIDIA drivers on pre-created GPU node", "nodeName", standardLinuxGPUNodeName)
					devicePluginTest := addonEc2Test.NewNvidiaDevicePluginTest(standardLinuxGPUNodeName)
					Expect(devicePluginTest.WaitForNvidiaDriverReady(ctx)).NotTo(HaveOccurred(), "NVIDIA drivers should be ready")
					Expect(devicePluginTest.Create(ctx)).To(Succeed(), "nvidia device plugin should have created successfully")
					Expect(devicePluginTest.Validate(ctx)).To(Succeed(), "nvidia device plugin should have been validated successfully")
					Expect(devicePluginTest.Delete(ctx)).To(Succeed(), "should clean up nvidia device plugin")
				}, Label("regular-os"))

				It("uses Bottlerocket OS", func(ctx context.Context) {
					if !gpuNodesCreated {
						Skip("GPU nodes were not created, skipping nvidia device plugin test")
					}
					// wait for nvidia drivers to be installed
					addonEc2Test.Logger.Info("Checking NVIDIA drivers on pre-created Bottlerocket GPU node", "nodeName", bottlerocketGPUNodeName)
					devicePluginTest := addonEc2Test.NewNvidiaDevicePluginTest(bottlerocketGPUNodeName)
					devicePluginTest.Command = "sudo /usr/sbin/chroot /.bottlerocket/rootfs/ nvidia-smi"
					devicePluginTest.CommandRunner = ssm.NewBottlerocketSSHOnSSMCommandRunner(addonEc2Test.SSMClient, addonEc2Test.JumpboxInstanceId, addonEc2Test.Logger)

					Expect(devicePluginTest.WaitForNvidiaDriverReady(ctx)).NotTo(HaveOccurred(), "NVIDIA drivers should be ready")
				}, Label("bottlerocket"))
			}, Label("nvidia-device-plugin"))

			Context("runs cert manager and AWS PCA issuer tests", Ordered, func() {
				It("uses regular OS (Ubuntu, AL, RHEL)", func(ctx context.Context) {
					certManager, err := addonEc2Test.NewCertManagerTest(ctx)
					Expect(err).To(Succeed(), "should have created cert-manager test")

					DeferCleanup(func(ctx context.Context) {
						Expect(certManager.Delete(ctx)).To(Succeed(), "should cleanup cert manager successfully")
					})

					skipIfAddonNotAvailable(certManager.Create(ctx), certManager.AddonName())

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
						Succeed(), "cert manager and AWS PCA issuer should have been validated successfully",
					)
				}, Label("regular-os"))

				It("uses Bottlerocket OS", func(ctx context.Context) {
					certManager, err := addonEc2Test.NewCertManagerTest(ctx)
					Expect(err).To(Succeed(), "should have created cert-manager test for bottlerocket")
					certManager.CertName = "bottlerocket-test-cert"
					certManager.CertNamespace = "bottlerocket-cert-test"
					certManager.CertSecretName = "bottlerocket-selfsigned-cert-tls"
					certManager.IssuerName = "bottlerocket-selfsigned-issuer"
					certManager.PCAIssuer.Namespace = "bottlerocket-cert-test"

					DeferCleanup(func(ctx context.Context) {
						Expect(certManager.Delete(ctx)).To(Succeed(), "should cleanup cert manager successfully")
					})

					skipIfAddonNotAvailable(certManager.Create(ctx), certManager.AddonName())

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
				}, Label("bottlerocket"))
			}, Label("cert-manager"))

			// Context("runs S3 mountpoint CSI driver tests", func() {
			// 	It("uses all OS", func(ctx context.Context) {
			// 		s3MountpointTest, err := addonEc2Test.NewS3MountpointCSIDriverTest(ctx)
			// 		Expect(err).To(Succeed(), "should have created s3 mountpoint test")

			// 		DeferCleanup(func(ctx context.Context) {
			// 			Expect(s3MountpointTest.Delete(ctx)).To(Succeed(), "should cleanup S3 mountpoint CSI driver successfully")
			// 		})

			// 		Expect(s3MountpointTest.Create(ctx)).To(
			// 			Succeed(), "S3 mountpoint CSI driver should have been created successfully",
			// 		)

			// 		Expect(s3MountpointTest.Validate(ctx)).To(
			// 			Succeed(), "S3 mountpoint CSI driver should have been validated successfully",
			// 		)
			// 	})
			// }, Label("s3-mountpoint-csi-driver"))

			Context("runs Secrets Store CSI driver tests", func() {
				It("uses all OS", func(ctx context.Context) {
					secretsStoreTest, err := addonEc2Test.NewSecretsStoreCSIDriverTest(ctx)
					Expect(err).To(Succeed(), "should have created secrets store CSI driver test")

					DeferCleanup(func(ctx context.Context) {
						Expect(secretsStoreTest.Delete(ctx)).To(Succeed(), "should cleanup Secrets Store CSI driver successfully")
					})

					skipIfAddonNotAvailable(secretsStoreTest.Create(ctx), secretsStoreTest.AddonName())

					Expect(secretsStoreTest.Validate(ctx)).To(
						Succeed(), "Secrets Store CSI driver should have been validated successfully",
					)
				})
			}, Label("secrets-store-csi-driver"))

			Context("runs external-dns tests", func() {
				It("uses all OS", func(ctx context.Context) {
					externalDNSTest, err := addonEc2Test.NewExternalDNSTest(ctx)
					Expect(err).To(Succeed(), "should have created external-dns test")

					DeferCleanup(func(ctx context.Context) {
						Expect(externalDNSTest.Delete(ctx)).To(Succeed(), "should cleanup external-dns successfully")
					})

					skipIfAddonNotAvailable(externalDNSTest.Create(ctx), externalDNSTest.AddonName())

					Expect(externalDNSTest.Validate(ctx)).To(
						Succeed(), "external-dns should have been validated successfully",
					)
				})
			}, Label("external-dns"))

			Context("runs FSx CSI driver tests", func() {
				It("uses all OS", func(ctx context.Context) {
					fsxCSITest, err := addonEc2Test.NewFsxCSIDriverTest(ctx)
					Expect(err).To(Succeed(), "should have created FSx CSI driver test")

					DeferCleanup(func(ctx context.Context) {
						Expect(fsxCSITest.Delete(ctx)).To(Succeed(), "should cleanup FSx CSI driver successfully")
					})

					skipIfAddonNotAvailable(fsxCSITest.Create(ctx), fsxCSITest.AddonName())

					Expect(fsxCSITest.Validate(ctx)).To(
						Succeed(), "FSx CSI driver should have been validated successfully",
					)
				})
			}, Label("fsx-csi-driver"))

			// Network Flow Monitor addon test - commented out until hybrid support is enabled
			// Context("runs network flow monitor tests", func() {
			// 	It("uses all OS", func(ctx context.Context) {
			// 		networkFlowMonitor, err := addonEc2Test.NewNetworkFlowMonitorTest(ctx)
			// 		Expect(err).To(Succeed(), "should have created network flow monitor test")
			//
			// 		DeferCleanup(func(ctx context.Context) {
			// 			Expect(networkFlowMonitor.Delete(ctx)).To(Succeed(), "should cleanup network flow monitor successfully")
			// 		})
			//
			// 		Expect(networkFlowMonitor.Create(ctx)).To(
			// 			Succeed(), "network flow monitor should have created successfully",
			// 		)
			//
			// 		DeferCleanup(func(ctx context.Context) {
			// 			report := CurrentSpecReport()
			// 			if report.State.Is(types.SpecStateFailed) {
			// 				err := networkFlowMonitor.PrintLogs(ctx)
			// 				if err != nil {
			// 					GinkgoWriter.Printf("Failed to get network flow monitor logs: %v\n", err)
			// 				}
			// 			}
			// 		})
			//
			// 		Expect(networkFlowMonitor.Validate(ctx)).To(
			// 			Succeed(), "network flow monitor should have been validated successfully",
			// 		)
			// 	})
			// }, Label("network-flow-monitor"))
		})
	})
})

// skipIfAddonNotAvailable skips the current test if err wraps ErrAddonNotAvailable,
// otherwise it fails the test with the error.
func skipIfAddonNotAvailable(err error, addonName string) {
	if errors.Is(err, addon.ErrAddonNotAvailable) {
		Skip(fmt.Sprintf("Addon %q is not available in this region, skipping test", addonName))
	}
	Expect(err).To(Succeed(), fmt.Sprintf("%s should have been created successfully", addonName))
}

func getRandomElements[E any](elems []E) (E, E) {
	length := len(elems)
	if length < 2 {
		return elems[0], elems[0]
	}

	rand.Shuffle(len(elems), func(i, j int) {
		elems[i], elems[j] = elems[j], elems[i]
	})

	first := rand.IntN(length)
	second := rand.IntN(length)

	for second == first {
		second = rand.IntN(length)
	}

	return elems[first], elems[second]
}
