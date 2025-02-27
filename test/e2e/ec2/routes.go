package ec2

import (
	"context"
	"fmt"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/ec2"
)

// CreateRouteForCIDRToInstance creates a route entry in the table routing a cidr to an instance.
func CreateRouteForCIDRToInstance(ctx context.Context, client *ec2.Client, routeTableID, cidr, instanceID string) error {
	_, err := client.CreateRoute(ctx, &ec2.CreateRouteInput{
		RouteTableId:         aws.String(routeTableID),
		DestinationCidrBlock: aws.String(cidr),
		InstanceId:           aws.String(instanceID),
	})
	if err != nil {
		return fmt.Errorf("could not create route to instance %s for dst CIDR %s: %w", instanceID, cidr, err)
	}
	return nil
}
