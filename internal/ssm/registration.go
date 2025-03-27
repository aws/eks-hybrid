package ssm

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"

	"github.com/aws/aws-sdk-go-v2/config"
	awsSsm "github.com/aws/aws-sdk-go-v2/service/ssm"
	"github.com/pkg/errors"
	"go.uber.org/zap"
)

const registrationFilePath = "/var/lib/amazon/ssm/registration"

type SSMRegistration struct {
	installRoot string
}

type ISSMRegistration interface {
	SSMClient(ctx context.Context) (SSMClient, error)
	RegistrationFilePath() string
	GetManagedHybridInstanceId() (string, error)
}

type SSMRegistrationOption func(*SSMRegistration)

func NewSSMRegistration(opts ...SSMRegistrationOption) *SSMRegistration {
	c := &SSMRegistration{}
	for _, opt := range opts {
		opt(c)
	}
	return c
}

type SSMClient interface {
	DescribeInstanceInformation(ctx context.Context, params *awsSsm.DescribeInstanceInformationInput, optFns ...func(*awsSsm.Options)) (*awsSsm.DescribeInstanceInformationOutput, error)
	DeregisterManagedInstance(ctx context.Context, params *awsSsm.DeregisterManagedInstanceInput, optFns ...func(*awsSsm.Options)) (*awsSsm.DeregisterManagedInstanceOutput, error)
}

func WithInstallRoot(installRoot string) SSMRegistrationOption {
	return func(c *SSMRegistration) {
		c.installRoot = installRoot
	}
}

func Deregister(ctx context.Context, registration ISSMRegistration, logger *zap.Logger) error {
	instanceId, err := registration.GetManagedHybridInstanceId()

	// If uninstall is being run just after running install and before running init
	// SSM would not be fully installed and registered, hence it's not required to run
	// deregister instance.
	if err != nil && os.IsNotExist(err) {
		logger.Info("Skipping SSM deregistration - node is not registered")
		return nil
	}
	if err != nil {
		return errors.Wrapf(err, "reading ssm registration file")
	}

	ssmClient, err := registration.SSMClient(ctx)
	if err != nil {
		return errors.Wrapf(err, "getting ssm client")
	}
	managed, err := isInstanceManaged(ssmClient, instanceId)
	if err != nil {
		return errors.Wrapf(err, "getting managed instance information")
	}

	// Only deregister the instance if init/ssm init was run and
	// if instances is actively listed as managed
	if managed {
		if err := deregister(ssmClient, instanceId); err != nil {
			return errors.Wrapf(err, "deregistering ssm managed instance")
		}
	}
	return nil
}

func (r *SSMRegistration) getManagedHybridInstanceIdAndRegion() (string, string, error) {
	data, err := os.ReadFile(r.RegistrationFilePath())
	if err != nil {
		return "", "", err
	}

	var registration HybridInstanceRegistration
	err = json.Unmarshal(data, &registration)
	if err != nil {
		return "", "", err
	}
	return registration.ManagedInstanceID, registration.Region, nil
}

func (r *SSMRegistration) GetManagedHybridInstanceId() (string, error) {
	data, err := os.ReadFile(r.RegistrationFilePath())
	if err != nil {
		return "", err
	}

	var registration HybridInstanceRegistration
	err = json.Unmarshal(data, &registration)
	if err != nil {
		return "", err
	}
	return registration.ManagedInstanceID, nil
}

func (r *SSMRegistration) isRegistered() (bool, error) {
	_, err := r.GetManagedHybridInstanceId()
	if err != nil {
		if os.IsNotExist(err) {
			return false, nil
		}
		return false, fmt.Errorf("reading ssm registration file: %w", err)
	}
	return true, nil
}

// RegistrationFilePath returns the path to the SSM registration file
// If installRoot is not set, it will return the path starting from the disk root
func (r *SSMRegistration) RegistrationFilePath() string {
	return filepath.Join(r.installRoot, registrationFilePath)
}

func (r *SSMRegistration) SSMClient(ctx context.Context) (SSMClient, error) {
	opts := []func(*config.LoadOptions) error{}

	_, region, err := r.getManagedHybridInstanceIdAndRegion()
	if err != nil && !os.IsNotExist(err) {
		return nil, fmt.Errorf("reading ssm registration file: %w", err)
	}
	if region != "" {
		opts = append(opts, config.WithRegion(region))
	}
	awsConfig, err := config.LoadDefaultConfig(ctx, opts...)
	if err != nil {
		return nil, err
	}
	ssmClient := awsSsm.NewFromConfig(awsConfig)
	return ssmClient, nil
}
