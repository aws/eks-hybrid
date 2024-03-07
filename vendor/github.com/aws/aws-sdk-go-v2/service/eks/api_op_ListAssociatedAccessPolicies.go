// Code generated by smithy-go-codegen DO NOT EDIT.

package eks

import (
	"context"
	"fmt"
	awsmiddleware "github.com/aws/aws-sdk-go-v2/aws/middleware"
	"github.com/aws/aws-sdk-go-v2/service/eks/types"
	"github.com/aws/smithy-go/middleware"
	smithyhttp "github.com/aws/smithy-go/transport/http"
)

// Lists the access policies associated with an access entry.
func (c *Client) ListAssociatedAccessPolicies(ctx context.Context, params *ListAssociatedAccessPoliciesInput, optFns ...func(*Options)) (*ListAssociatedAccessPoliciesOutput, error) {
	if params == nil {
		params = &ListAssociatedAccessPoliciesInput{}
	}

	result, metadata, err := c.invokeOperation(ctx, "ListAssociatedAccessPolicies", params, optFns, c.addOperationListAssociatedAccessPoliciesMiddlewares)
	if err != nil {
		return nil, err
	}

	out := result.(*ListAssociatedAccessPoliciesOutput)
	out.ResultMetadata = metadata
	return out, nil
}

type ListAssociatedAccessPoliciesInput struct {

	// The name of your cluster.
	//
	// This member is required.
	ClusterName *string

	// The ARN of the IAM principal for the AccessEntry .
	//
	// This member is required.
	PrincipalArn *string

	// The maximum number of results, returned in paginated output. You receive
	// maxResults in a single page, along with a nextToken response element. You can
	// see the remaining results of the initial request by sending another request with
	// the returned nextToken value. This value can be between 1 and 100. If you don't
	// use this parameter, 100 results and a nextToken value, if applicable, are
	// returned.
	MaxResults *int32

	// The nextToken value returned from a previous paginated request, where maxResults
	// was used and the results exceeded the value of that parameter. Pagination
	// continues from the end of the previous results that returned the nextToken
	// value. This value is null when there are no more results to return. This token
	// should be treated as an opaque identifier that is used only to retrieve the next
	// items in a list and not for other programmatic purposes.
	NextToken *string

	noSmithyDocumentSerde
}

type ListAssociatedAccessPoliciesOutput struct {

	// The list of access policies associated with the access entry.
	AssociatedAccessPolicies []types.AssociatedAccessPolicy

	// The name of your cluster.
	ClusterName *string

	// The nextToken value returned from a previous paginated request, where maxResults
	// was used and the results exceeded the value of that parameter. Pagination
	// continues from the end of the previous results that returned the nextToken
	// value. This value is null when there are no more results to return. This token
	// should be treated as an opaque identifier that is used only to retrieve the next
	// items in a list and not for other programmatic purposes.
	NextToken *string

	// The ARN of the IAM principal for the AccessEntry .
	PrincipalArn *string

	// Metadata pertaining to the operation's result.
	ResultMetadata middleware.Metadata

	noSmithyDocumentSerde
}

func (c *Client) addOperationListAssociatedAccessPoliciesMiddlewares(stack *middleware.Stack, options Options) (err error) {
	if err := stack.Serialize.Add(&setOperationInputMiddleware{}, middleware.After); err != nil {
		return err
	}
	err = stack.Serialize.Add(&awsRestjson1_serializeOpListAssociatedAccessPolicies{}, middleware.After)
	if err != nil {
		return err
	}
	err = stack.Deserialize.Add(&awsRestjson1_deserializeOpListAssociatedAccessPolicies{}, middleware.After)
	if err != nil {
		return err
	}
	if err := addProtocolFinalizerMiddlewares(stack, options, "ListAssociatedAccessPolicies"); err != nil {
		return fmt.Errorf("add protocol finalizers: %v", err)
	}

	if err = addlegacyEndpointContextSetter(stack, options); err != nil {
		return err
	}
	if err = addSetLoggerMiddleware(stack, options); err != nil {
		return err
	}
	if err = addClientRequestID(stack); err != nil {
		return err
	}
	if err = addComputeContentLength(stack); err != nil {
		return err
	}
	if err = addResolveEndpointMiddleware(stack, options); err != nil {
		return err
	}
	if err = addComputePayloadSHA256(stack); err != nil {
		return err
	}
	if err = addRetry(stack, options); err != nil {
		return err
	}
	if err = addRawResponseToMetadata(stack); err != nil {
		return err
	}
	if err = addRecordResponseTiming(stack); err != nil {
		return err
	}
	if err = addClientUserAgent(stack, options); err != nil {
		return err
	}
	if err = smithyhttp.AddErrorCloseResponseBodyMiddleware(stack); err != nil {
		return err
	}
	if err = smithyhttp.AddCloseResponseBodyMiddleware(stack); err != nil {
		return err
	}
	if err = addSetLegacyContextSigningOptionsMiddleware(stack); err != nil {
		return err
	}
	if err = addOpListAssociatedAccessPoliciesValidationMiddleware(stack); err != nil {
		return err
	}
	if err = stack.Initialize.Add(newServiceMetadataMiddleware_opListAssociatedAccessPolicies(options.Region), middleware.Before); err != nil {
		return err
	}
	if err = addRecursionDetection(stack); err != nil {
		return err
	}
	if err = addRequestIDRetrieverMiddleware(stack); err != nil {
		return err
	}
	if err = addResponseErrorMiddleware(stack); err != nil {
		return err
	}
	if err = addRequestResponseLogging(stack, options); err != nil {
		return err
	}
	if err = addDisableHTTPSMiddleware(stack, options); err != nil {
		return err
	}
	return nil
}

// ListAssociatedAccessPoliciesAPIClient is a client that implements the
// ListAssociatedAccessPolicies operation.
type ListAssociatedAccessPoliciesAPIClient interface {
	ListAssociatedAccessPolicies(context.Context, *ListAssociatedAccessPoliciesInput, ...func(*Options)) (*ListAssociatedAccessPoliciesOutput, error)
}

var _ ListAssociatedAccessPoliciesAPIClient = (*Client)(nil)

// ListAssociatedAccessPoliciesPaginatorOptions is the paginator options for
// ListAssociatedAccessPolicies
type ListAssociatedAccessPoliciesPaginatorOptions struct {
	// The maximum number of results, returned in paginated output. You receive
	// maxResults in a single page, along with a nextToken response element. You can
	// see the remaining results of the initial request by sending another request with
	// the returned nextToken value. This value can be between 1 and 100. If you don't
	// use this parameter, 100 results and a nextToken value, if applicable, are
	// returned.
	Limit int32

	// Set to true if pagination should stop if the service returns a pagination token
	// that matches the most recent token provided to the service.
	StopOnDuplicateToken bool
}

// ListAssociatedAccessPoliciesPaginator is a paginator for
// ListAssociatedAccessPolicies
type ListAssociatedAccessPoliciesPaginator struct {
	options   ListAssociatedAccessPoliciesPaginatorOptions
	client    ListAssociatedAccessPoliciesAPIClient
	params    *ListAssociatedAccessPoliciesInput
	nextToken *string
	firstPage bool
}

// NewListAssociatedAccessPoliciesPaginator returns a new
// ListAssociatedAccessPoliciesPaginator
func NewListAssociatedAccessPoliciesPaginator(client ListAssociatedAccessPoliciesAPIClient, params *ListAssociatedAccessPoliciesInput, optFns ...func(*ListAssociatedAccessPoliciesPaginatorOptions)) *ListAssociatedAccessPoliciesPaginator {
	if params == nil {
		params = &ListAssociatedAccessPoliciesInput{}
	}

	options := ListAssociatedAccessPoliciesPaginatorOptions{}
	if params.MaxResults != nil {
		options.Limit = *params.MaxResults
	}

	for _, fn := range optFns {
		fn(&options)
	}

	return &ListAssociatedAccessPoliciesPaginator{
		options:   options,
		client:    client,
		params:    params,
		firstPage: true,
		nextToken: params.NextToken,
	}
}

// HasMorePages returns a boolean indicating whether more pages are available
func (p *ListAssociatedAccessPoliciesPaginator) HasMorePages() bool {
	return p.firstPage || (p.nextToken != nil && len(*p.nextToken) != 0)
}

// NextPage retrieves the next ListAssociatedAccessPolicies page.
func (p *ListAssociatedAccessPoliciesPaginator) NextPage(ctx context.Context, optFns ...func(*Options)) (*ListAssociatedAccessPoliciesOutput, error) {
	if !p.HasMorePages() {
		return nil, fmt.Errorf("no more pages available")
	}

	params := *p.params
	params.NextToken = p.nextToken

	var limit *int32
	if p.options.Limit > 0 {
		limit = &p.options.Limit
	}
	params.MaxResults = limit

	result, err := p.client.ListAssociatedAccessPolicies(ctx, &params, optFns...)
	if err != nil {
		return nil, err
	}
	p.firstPage = false

	prevToken := p.nextToken
	p.nextToken = result.NextToken

	if p.options.StopOnDuplicateToken &&
		prevToken != nil &&
		p.nextToken != nil &&
		*prevToken == *p.nextToken {
		p.nextToken = nil
	}

	return result, nil
}

func newServiceMetadataMiddleware_opListAssociatedAccessPolicies(region string) *awsmiddleware.RegisterServiceMetadata {
	return &awsmiddleware.RegisterServiceMetadata{
		Region:        region,
		ServiceID:     ServiceID,
		OperationName: "ListAssociatedAccessPolicies",
	}
}
