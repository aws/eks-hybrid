package constants

import "time"

const (
	CreationTimeTagKey              = "CreationTime"
	TestClusterTagKey               = "Nodeadm-E2E-Tests-Cluster"
	TestRolePathPrefix              = "/NodeadmE2E/"
	EcrAccounId                     = "381492195191"
	LogCollectorBundleFileName      = "bundle.tar.gz"
	TestCredentialsStackNamePrefix  = "EKSHybridCI"
	TestArchitectureStackNamePrefix = "EKSHybridCI-Arch"
	TestInstanceName                = "TestInstanceName"
	TestArtifactsPath               = "TestArtifactsPath"
	TestConformancePath             = "TestConformancePath"
	TestLogBundleFile               = "TestLogBundleFile"
	TestSerialOutputLogFile         = "TestSerialOutputLogFile"
	TestNodeadmVersion              = "TestNodeadmVersion"
	TestS3LogsFolder                = "logs"
	SerialOutputLogFile             = "serial-output.log"
	TestInstanceNameKubernetesLabel = "test.eks-hybrid.amazonaws.com/node-name"
	DeferCleanupTimeout             = 5 * time.Minute
)
