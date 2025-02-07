package creds_test

import (
	"fmt"
	"testing"

	. "github.com/onsi/gomega"

	"github.com/aws/eks-hybrid/internal/creds"
	"github.com/aws/eks-hybrid/internal/system"
)

func TestValidateCredentialProvider(t *testing.T) {
	g := NewGomegaWithT(t)
	testcases := []struct {
		name               string
		credentialProvider creds.CredentialProvider
		osName             string
		osVersion          string
		wantErr            error
	}{
		{
			name:               "SSM provider - happy path",
			credentialProvider: creds.SsmCredentialProvider,
			osName:             "random",
			osVersion:          "randomVersion",
			wantErr:            nil,
		},
		{
			name:               "IAM RA - RHEL 8",
			credentialProvider: creds.IamRolesAnywhereCredentialProvider,
			osName:             system.RhelOsName,
			osVersion:          "8.10",
			wantErr:            fmt.Errorf("iam-ra credential provider is not supported on %s %s based operating systems. Please use ssm credential provider", system.RhelOsName, "8.10"),
		},
		{
			name:               "IAM RA - Ubuntu 20",
			credentialProvider: creds.IamRolesAnywhereCredentialProvider,
			osName:             system.UbuntuOsName,
			osVersion:          "20.23",
			wantErr:            fmt.Errorf("iam-ra credential provider is not supported on %s %s based operating systems. Please use ssm credential provider", system.UbuntuOsName, "20.23"),
		},
		{
			name:               "IAM RA - Happy OSes",
			credentialProvider: creds.IamRolesAnywhereCredentialProvider,
			osName:             system.AmazonOsName,
			osVersion:          "3.2.4",
			wantErr:            nil,
		},
		{
			name:               "IAM RA - Ubuntu 22",
			credentialProvider: creds.IamRolesAnywhereCredentialProvider,
			osName:             system.UbuntuOsName,
			osVersion:          "22.23",
			wantErr:            nil,
		},
		{
			name:               "IAM RA - RHEL 9",
			credentialProvider: creds.IamRolesAnywhereCredentialProvider,
			osName:             system.RhelOsName,
			osVersion:          "9.10",
			wantErr:            nil,
		},
	}
	for _, tc := range testcases {
		t.Run(tc.name, func(t *testing.T) {
			err := creds.ValidateCredentialProvider(tc.credentialProvider, tc.osName, tc.osVersion)
			if tc.wantErr == nil {
				g.Expect(err).To(BeNil())
			} else {
				g.Expect(err).To(Equal(tc.wantErr))
			}
		})
	}
}
