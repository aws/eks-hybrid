package constants

import "time"

const (
	CreationTimeTagKey = "CreationTime"
	TestClusterTagKey  = "Nodeadm-E2E-Tests-Cluster"
	OSArchTagKey       = "OS-Arch"
	TestRolePathPrefix = "/NodeadmE2E/"
	EcrAccountId       = "381492195191"
	// ChinaEcrAccountId is the ECR account ID used in AWS China (aws-cn) regions for test images.
	ChinaEcrAccountId = "858475954877"
	// ChinaCiliumEcrAccountId is the ECR account ID used in AWS China (aws-cn) regions for Cilium images.
	// This account mirrors Cilium images for use in China regions.
	ChinaCiliumEcrAccountId = "907723705730"
	// PublicEcrRegistry is the public ECR registry. It is anonymously pullable from every
	// partition, including AWS China (aws-cn).
	PublicEcrRegistry = "public.ecr.aws"
	// CiliumPublicEcrRegistry is the public ECR registry used for Cilium images in standard regions.
	CiliumPublicEcrRegistry         = PublicEcrRegistry + "/eks"
	LogCollectorBundleFileName      = "bundle.tar.gz"
	JumpboxLogBundleFileName        = "jumpbox-bundle.tar.gz"
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
	RolesAnywhereCertPath           = "/etc/roles-anywhere/pki/node.crt"
	RolesAnywhereKeyPath            = "/etc/roles-anywhere/pki/node.key"
	BottlerocketOsName              = "bottlerocket"
)
