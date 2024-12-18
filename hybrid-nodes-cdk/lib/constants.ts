export const builderBaseImage = 'public.ecr.aws/eks-distro-build-tooling/builder-base:standard-latest.al2';
export const kubernetesVersions = ['1.25', '1.26', '1.27', '1.28', '1.29', '1.30', '1.31'];
export const cnis = ['calico', 'cilium'];
export const eksHybridBetaBucketARN = 'arn:aws:s3:::eks-hybrid-beta';
export const eksReleaseManifestHost = 'hybrid-assets.eks.amazonaws.com';
export const githubRepo = 'eks-hybrid';
export const githubBranch = 'main';
export const requiredEnvVars = ['HYBRID_GITHUB_TOKEN'];
