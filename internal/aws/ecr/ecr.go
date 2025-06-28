package ecr

import (
	"context"
	"fmt"
	"net"
	"strings"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/ecr"

	awsinternal "github.com/aws/eks-hybrid/internal/aws"
	"github.com/aws/eks-hybrid/internal/aws/imds"
	"github.com/aws/eks-hybrid/internal/system"
)

const hybridServicesDomain = "amazonaws.com"

// Returns the base64 encoded authorization token string for ECR of the format "AWS:XXXXX"
func GetAuthorizationToken(awsConfig *aws.Config) (string, error) {
	ecrClient := ecr.NewFromConfig(*awsConfig)
	token, err := ecrClient.GetAuthorizationToken(context.Background(), &ecr.GetAuthorizationTokenInput{})
	if err != nil {
		return "", err
	}
	authData := token.AuthorizationData[0].AuthorizationToken
	return *authData, nil
}

func (r *ECRRegistry) GetSandboxImage() string {
	return r.GetImageReference("eks/pause", "3.5")
}

func GetEKSRegistry(region string, regionConfig *awsinternal.RegionData) (ECRRegistry, error) {
	servicesDomain, err := imds.GetProperty(imds.ServicesDomain)
	if err != nil {
		return "", err
	}

	return getEksRegistryWithServiceDomain(region, servicesDomain, regionConfig)
}

func GetEKSHybridRegistry(region string, regionConfig *awsinternal.RegionData) (ECRRegistry, error) {
	return getEksRegistryWithServiceDomain(region, hybridServicesDomain, regionConfig)
}

func getEksRegistryWithServiceDomain(region, servicesDomain string, regionConfig *awsinternal.RegionData) (ECRRegistry, error) {
	account, region := getEKSRegistryCoordinates(region, regionConfig)
	fipsInstalled, fipsEnabled, err := system.GetFipsInfo()
	if err != nil {
		return "", err
	}
	if fipsInstalled && fipsEnabled {
		fipsRegistry := getRegistry(account, "ecr-fips", region, servicesDomain)
		if addresses, err := net.LookupHost(fipsRegistry); err != nil {
			return "", err
		} else if len(addresses) > 0 {
			return ECRRegistry(fipsRegistry), nil
		}
	}
	return ECRRegistry(getRegistry(account, "ecr", region, servicesDomain)), nil
}

type ECRRegistry string

func (r *ECRRegistry) String() string {
	return string(*r)
}

func (r *ECRRegistry) GetImageReference(repository, tag string) string {
	return fmt.Sprintf("%s/%s:%s", r.String(), repository, tag)
}

func getRegistry(accountID, ecrSubdomain, region, servicesDomain string) string {
	return fmt.Sprintf("%s.dkr.%s.%s.%s", accountID, ecrSubdomain, region, servicesDomain)
}

const nonOptInRegionAccount = "602401143452"

var accountsByRegion = map[string]string{
	"cn-north-1":      "918309763551",
	"cn-northwest-1":  "961992271922",
	"eu-isoe-west-1":  "249663109785",
	"us-gov-east-1":   "151742754352",
	"us-gov-west-1":   "013241004608",
	"us-iso-east-1":   "725322719131",
	"us-iso-west-1":   "608367168043",
	"us-isob-east-1":  "187977181151",
	"us-isof-south-1": "676585237158",
}

// getEKSRegistryCoordinates returns an AWS region and account ID for the default EKS ECR container image registry
func getEKSRegistryCoordinates(region string, regionConfig *awsinternal.RegionData) (string, string) {
	if regionConfig != nil && regionConfig.EcrAccountID != "" {
		return regionConfig.EcrAccountID, region
	}

	inRegionRegistry, ok := accountsByRegion[region]
	if ok {
		return inRegionRegistry, region
	}

	// Fallback to existing region prefix-based logic
	if strings.HasPrefix(region, "us-gov-") {
		return "013241004608", "us-gov-west-1"
	} else if strings.HasPrefix(region, "cn-") {
		return "961992271922", "cn-northwest-1"
	} else if strings.HasPrefix(region, "us-iso-") {
		return "725322719131", "us-iso-east-1"
	} else if strings.HasPrefix(region, "us-isob-") {
		return "187977181151", "us-isob-east-1"
	} else if strings.HasPrefix(region, "us-isof-") {
		return "676585237158", "us-isof-south-1"
	}
	return nonOptInRegionAccount, "us-west-2"
}
