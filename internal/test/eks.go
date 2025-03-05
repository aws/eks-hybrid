package test

import (
	"net/http"
	"testing"

	eks_sdk "github.com/aws/aws-sdk-go-v2/service/eks"
)

// NewEKSDescribeClusterAPI creates a new TestServer that behaves like the EKS DescribeCluster API.
func NewEKSDescribeClusterAPI(tb testing.TB, resp *eks_sdk.DescribeClusterOutput) TestServer {
	return NewHTTPSServerForJSON(tb, http.StatusOK, resp)
}
