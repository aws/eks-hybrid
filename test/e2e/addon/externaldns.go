package addon

import (
	"context"
	_ "embed"
	"fmt"
	"strings"
	"time"

	"github.com/aws/aws-sdk-go-v2/service/eks"
	"github.com/aws/aws-sdk-go-v2/service/route53"
	"github.com/aws/aws-sdk-go-v2/service/route53/types"
	"github.com/go-logr/logr"
	"k8s.io/apimachinery/pkg/util/wait"
	"k8s.io/client-go/rest"

	"github.com/aws/eks-hybrid/test/e2e/constants"
	"github.com/aws/eks-hybrid/test/e2e/kubernetes"
	peeredtypes "github.com/aws/eks-hybrid/test/e2e/peered/types"
)

//go:embed testdata/nginx-external-dns.yaml
var testServiceYAML string

const (
	externalDNS               = "external-dns"
	externalDNSNamespace      = "external-dns"
	externalDNSDeployment     = "external-dns"
	externalDNSServiceAccount = "external-dns"
	externalDNSTestService    = "nginx"
	externalDNSWaitTimeout    = 5 * time.Minute
	externalDNSPollInterval   = 20 * time.Second
)

// ExternalDNSTest tests the external-dns addon
type ExternalDNSTest struct {
	Cluster            string
	addon              *Addon
	K8S                peeredtypes.K8s
	EKSClient          *eks.Client
	Route53Client      *route53.Client
	K8SConfig          *rest.Config
	Logger             logr.Logger
	PodIdentityRoleArn string

	hostedZoneId   *string
	hostedZoneName *string
}

// Create installs the external-dns addon
func (e *ExternalDNSTest) Create(ctx context.Context) error {
	hostedZoneId, err := e.getHostedZoneId(ctx)
	if err != nil {
		return fmt.Errorf("failed to get hosted zone id: %w", err)
	}

	hostedZoneName, err := e.getHostedZoneName(ctx, hostedZoneId)
	if err != nil {
		return fmt.Errorf("failed to get hosted zone name: %w", err)
	}

	e.hostedZoneId = hostedZoneId
	e.hostedZoneName = hostedZoneName
	e.Logger.Info("Hosted zone", "Id", hostedZoneId, "Name", hostedZoneName)

	configuration := fmt.Sprintf(`{"domainFilters": ["%s"], "policy": "sync"}`, *hostedZoneName)
	e.addon = &Addon{
		Cluster:       e.Cluster,
		Namespace:     externalDNSNamespace,
		Name:          externalDNS,
		Configuration: configuration,
		PodIdentityAssociations: []PodIdentityAssociation{
			{
				RoleArn:        e.PodIdentityRoleArn,
				ServiceAccount: externalDNSServiceAccount,
			},
		},
	}

	if err := e.addon.CreateAndWaitForActive(ctx, e.EKSClient, e.K8S, e.Logger); err != nil {
		return err
	}

	// Wait for external-dns deployment to be ready
	if err := kubernetes.DeploymentWaitForReplicas(ctx, externalDNSWaitTimeout, e.K8S, externalDNSNamespace, externalDNSDeployment); err != nil {
		return fmt.Errorf("deployment %s not ready: %w", externalDNSDeployment, err)
	}

	return nil
}

// Validate checks if external-dns is working correctly
func (e *ExternalDNSTest) Validate(ctx context.Context) error {
	if e.hostedZoneName == nil {
		return fmt.Errorf("hosted zone name is not set, ensure Create() was called first")
	}

	e.Logger.Info("Deploying test service for external-dns validation")

	// Remove the trailing dot from the hosted zone name if present
	hostedZoneName := strings.TrimSuffix(*e.hostedZoneName, ".")

	replacer := strings.NewReplacer(
		"{{TEST_SERVICE}}", externalDNSTestService,
		"{{NAMESPACE}}", defaultNamespace,
		"{{HOSTED_ZONE_NAME}}", hostedZoneName,
	)
	replacedTestServiceYAML := replacer.Replace(testServiceYAML)

	// Deploy the test service with external-dns annotation
	objs, err := kubernetes.YamlToUnstructured([]byte(replacedTestServiceYAML))
	if err != nil {
		return fmt.Errorf("failed to parse YAML: %w", err)
	}

	e.Logger.Info("Applying test service YAML")
	if err := kubernetes.UpsertManifestsWithRetries(ctx, e.K8S, objs); err != nil {
		return fmt.Errorf("failed to deploy test service: %w", err)
	}

	// Wait for deployment to be ready
	if err := kubernetes.DeploymentWaitForReplicas(ctx, externalDNSWaitTimeout, e.K8S, defaultNamespace, externalDNSTestService); err != nil {
		return fmt.Errorf("service deployment not ready: %w", err)
	}

	// Wait for external-dns to create the DNS record
	expectedDNSName := fmt.Sprintf("%s.%s", externalDNSTestService, hostedZoneName)
	e.Logger.Info("Waiting for external-dns to create DNS record", "dnsName", expectedDNSName)

	if err := e.waitForDNSRecord(ctx, expectedDNSName); err != nil {
		return fmt.Errorf("DNS record not created by external-dns: %w", err)
	}

	// Clean up - delete test service
	e.Logger.Info("Cleaning up test service")
	if err := kubernetes.DeleteManifestsWithRetries(ctx, e.K8S, objs); err != nil {
		return fmt.Errorf("failed to delete test service: %w", err)
	}

	// Wait for external-dns to clean up the DNS record
	e.Logger.Info("Waiting for external-dns to clean up DNS record", "dnsName", expectedDNSName)
	if err := e.waitForDNSRecordDeletion(ctx, expectedDNSName); err != nil {
		return fmt.Errorf("DNS record not cleaned up by external-dns: %w", err)
	}

	e.Logger.Info("Successfully validated external-dns functionality", "dnsName", expectedDNSName)
	return nil
}

func (e *ExternalDNSTest) Delete(ctx context.Context) error {
	return e.addon.Delete(ctx, e.EKSClient, e.Logger)
}

func (e *ExternalDNSTest) getHostedZoneId(ctx context.Context) (*string, error) {
	output, err := e.Route53Client.ListHostedZones(ctx, &route53.ListHostedZonesInput{
		HostedZoneType: types.HostedZoneTypePrivateHostedZone,
	})
	if err != nil {
		return nil, fmt.Errorf("failed to list hosted zones: %w", err)
	}

	var hostedZoneIds []string

	for _, hostedZone := range output.HostedZones {
		hostedZoneIds = append(hostedZoneIds, strings.Split(*hostedZone.Id, "/")[2])
	}

	listTagsOutput, err := e.Route53Client.ListTagsForResources(ctx, &route53.ListTagsForResourcesInput{
		ResourceIds:  hostedZoneIds,
		ResourceType: types.TagResourceTypeHostedzone,
	})
	if err != nil {
		return nil, fmt.Errorf("failed to list tags for hosted zones: %w", err)
	}

	for _, resourceTagSet := range listTagsOutput.ResourceTagSets {
		for _, tag := range resourceTagSet.Tags {
			if *tag.Key == constants.TestClusterTagKey && *tag.Value == e.Cluster {
				return resourceTagSet.ResourceId, nil
			}
		}
	}

	return nil, fmt.Errorf("hosted zone not found for cluster %s", e.Cluster)
}

func (e *ExternalDNSTest) getHostedZoneName(ctx context.Context, hostedZoneId *string) (*string, error) {
	zoneOutput, err := e.Route53Client.GetHostedZone(ctx, &route53.GetHostedZoneInput{
		Id: hostedZoneId,
	})
	if err != nil {
		return nil, fmt.Errorf("failed to get hosted zone: %w", err)
	}

	return zoneOutput.HostedZone.Name, nil
}

func (e *ExternalDNSTest) waitForDNSRecord(ctx context.Context, expectedDNSName string) error {
	if e.hostedZoneId == nil {
		return fmt.Errorf("hosted zone id is not set, ensure Create() was called first")
	}

	e.Logger.Info("Polling for DNS record creation", "dnsName", expectedDNSName)

	err := wait.PollUntilContextTimeout(ctx, externalDNSPollInterval, externalDNSWaitTimeout, true, func(ctx context.Context) (bool, error) {
		found, err := e.checkDNSRecordExists(ctx, e.hostedZoneId, expectedDNSName)
		if err != nil {
			e.Logger.Error(err, "Error checking DNS record", "dnsName", expectedDNSName)
			return false, nil // Continue polling on error
		}
		if found {
			e.Logger.Info("DNS record found in hosted zone", "dnsName", expectedDNSName)
			return true, nil
		}
		e.Logger.Info("DNS record not found yet, waiting...", "dnsName", expectedDNSName)
		return false, nil
	})
	if err != nil {
		return fmt.Errorf("timeout waiting for DNS record %s to be created: %w", expectedDNSName, err)
	}

	return nil
}

func (e *ExternalDNSTest) waitForDNSRecordDeletion(ctx context.Context, expectedDNSName string) error {
	if e.hostedZoneId == nil {
		return fmt.Errorf("hosted zone id is not set, ensure Create() was called first")
	}

	e.Logger.Info("Polling for DNS record deletion", "dnsName", expectedDNSName)

	err := wait.PollUntilContextTimeout(ctx, externalDNSPollInterval, externalDNSWaitTimeout, true, func(ctx context.Context) (bool, error) {
		found, err := e.checkDNSRecordExists(ctx, e.hostedZoneId, expectedDNSName)
		if err != nil {
			e.Logger.Error(err, "Error checking DNS record", "dnsName", expectedDNSName)
			return false, nil // Continue polling on error
		}
		if !found {
			e.Logger.Info("DNS record successfully deleted from hosted zone", "dnsName", expectedDNSName)
			return true, nil
		}
		e.Logger.Info("DNS record still exists, waiting for deletion...", "dnsName", expectedDNSName)
		return false, nil
	})
	if err != nil {
		return fmt.Errorf("timeout waiting for DNS record %s to be deleted: %w", expectedDNSName, err)
	}

	return nil
}

func (e *ExternalDNSTest) checkDNSRecordExists(ctx context.Context, hostedZoneId *string, dnsName string) (bool, error) {
	// Ensure DNS name ends with a dot for Route53 API
	if !strings.HasSuffix(dnsName, ".") {
		dnsName = dnsName + "."
	}

	listInput := &route53.ListResourceRecordSetsInput{
		HostedZoneId: hostedZoneId,
	}

	output, err := e.Route53Client.ListResourceRecordSets(ctx, listInput)
	if err != nil {
		return false, fmt.Errorf("failed to list resource record sets: %w", err)
	}

	for _, recordSet := range output.ResourceRecordSets {
		if recordSet.Name != nil && *recordSet.Name == dnsName {
			// Found the DNS record, check if it's an A record (LoadBalancer creates A records)
			if recordSet.Type == types.RRTypeA {
				return true, nil
			}
		}
	}

	return false, nil
}
