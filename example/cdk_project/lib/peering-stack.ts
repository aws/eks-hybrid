import * as cdk from 'aws-cdk-lib';
import { Construct } from 'constructs';
import * as ec2 from 'aws-cdk-lib/aws-ec2';

interface PeeringStackProps extends cdk.StackProps {
    clusterVPC: ec2.Vpc;
    remoteVPC: ec2.Vpc;
}

export class PeeringStack extends cdk.Stack {
    constructor(scope: Construct, id: string, props: PeeringStackProps) {
        super(scope, id, props);

        const clusterVPC = props.clusterVPC;
        const remoteVPC = props.remoteVPC;

        const vpcPeeringConnection = new ec2.CfnVPCPeeringConnection(this, 'Cluster-Remote-Peering', {
            vpcId: clusterVPC.vpcId,
            peerVpcId: remoteVPC.vpcId,
            peerRegion: this.region,
        });

        clusterVPC.publicSubnets.forEach((subnet, index)=>{
            const routeTable = subnet.routeTable;
            new ec2.CfnRoute(this, `RouteFromClusterToRemote-${index}`, {
                routeTableId: routeTable.routeTableId,
                destinationCidrBlock: remoteVPC.vpcCidrBlock,
                vpcPeeringConnectionId: vpcPeeringConnection.ref
            });
        });

        remoteVPC.publicSubnets.forEach((subnet,index)=>{
            const routeTable = subnet.routeTable;
            new ec2.CfnRoute(this, `RouteFromRemoteToCluster-${index}`, {
                routeTableId: routeTable.routeTableId,
                destinationCidrBlock: clusterVPC.vpcCidrBlock,
                vpcPeeringConnectionId: vpcPeeringConnection.ref
            });
        });
    }
}
