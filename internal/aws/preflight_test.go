package aws

import (
	"context"
	"errors"
	"testing"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/ec2"
	ec2types "github.com/aws/aws-sdk-go-v2/service/ec2/types"
	"github.com/aws/aws-sdk-go-v2/service/sts"
)

// mockSTSPreflightClient implements STSPreflightAPI
type mockSTSPreflightClient struct {
	output *sts.GetCallerIdentityOutput
	err    error
}

func (m *mockSTSPreflightClient) GetCallerIdentity(ctx context.Context, params *sts.GetCallerIdentityInput, optFns ...func(*sts.Options)) (*sts.GetCallerIdentityOutput, error) {
	if m.err != nil {
		return nil, m.err
	}
	return m.output, nil
}

// mockEC2PreflightClient implements EC2PreflightAPI
type mockEC2PreflightClient struct {
	vpcsOutput    *ec2.DescribeVpcsOutput
	vpcsErr       error
	subnetsOutput *ec2.DescribeSubnetsOutput
	subnetsErr    error
	imagesOutput  *ec2.DescribeImagesOutput
	imagesErr     error
	attrsOutput   *ec2.DescribeAccountAttributesOutput
	attrsErr      error
}

func (m *mockEC2PreflightClient) DescribeVpcs(ctx context.Context, params *ec2.DescribeVpcsInput, optFns ...func(*ec2.Options)) (*ec2.DescribeVpcsOutput, error) {
	if m.vpcsErr != nil {
		return nil, m.vpcsErr
	}
	return m.vpcsOutput, nil
}

func (m *mockEC2PreflightClient) DescribeSubnets(ctx context.Context, params *ec2.DescribeSubnetsInput, optFns ...func(*ec2.Options)) (*ec2.DescribeSubnetsOutput, error) {
	if m.subnetsErr != nil {
		return nil, m.subnetsErr
	}
	return m.subnetsOutput, nil
}

func (m *mockEC2PreflightClient) DescribeImages(ctx context.Context, params *ec2.DescribeImagesInput, optFns ...func(*ec2.Options)) (*ec2.DescribeImagesOutput, error) {
	if m.imagesErr != nil {
		return nil, m.imagesErr
	}
	return m.imagesOutput, nil
}

func (m *mockEC2PreflightClient) DescribeAccountAttributes(ctx context.Context, params *ec2.DescribeAccountAttributesInput, optFns ...func(*ec2.Options)) (*ec2.DescribeAccountAttributesOutput, error) {
	if m.attrsErr != nil {
		return nil, m.attrsErr
	}
	return m.attrsOutput, nil
}

func TestRunPreflightCheck(t *testing.T) {
	ctx := context.Background()

	t.Run("All 4 checks pass successfully", func(t *testing.T) {
		stsMock := &mockSTSPreflightClient{
			output: &sts.GetCallerIdentityOutput{
				Account: aws.String("123456789012"),
				Arn:     aws.String("arn:aws:iam::123456789012:user/admin"),
				UserId:  aws.String("AIDA1234567890"),
			},
		}

		ec2Mock := &mockEC2PreflightClient{
			vpcsOutput: &ec2.DescribeVpcsOutput{
				Vpcs: []ec2types.Vpc{
					{
						VpcId:     aws.String("vpc-default123"),
						IsDefault: aws.Bool(true),
						CidrBlock: aws.String("172.31.0.0/16"),
						Tags: []ec2types.Tag{
							{Key: aws.String("Name"), Value: aws.String("Default-VPC")},
						},
					},
				},
			},
			subnetsOutput: &ec2.DescribeSubnetsOutput{
				Subnets: []ec2types.Subnet{
					{
						SubnetId:                aws.String("subnet-12345"),
						VpcId:                   aws.String("vpc-default123"),
						AvailabilityZone:        aws.String("ap-south-1a"),
						MapPublicIpOnLaunch:     aws.Bool(true),
						AvailableIpAddressCount: aws.Int32(250),
					},
				},
			},
			imagesOutput: &ec2.DescribeImagesOutput{
				Images: []ec2types.Image{
					{
						ImageId:      aws.String("ami-0123456789abcdef0"),
						Name:         aws.String("ubuntu/images/ubuntu-noble-24.04-amd64"),
						State:        ec2types.ImageStateAvailable,
						Architecture: ec2types.ArchitectureValuesX8664,
					},
				},
			},
			attrsOutput: &ec2.DescribeAccountAttributesOutput{
				AccountAttributes: []ec2types.AccountAttribute{
					{AttributeName: aws.String("supported-platforms")},
				},
			},
		}

		report := RunPreflightCheckWithClients(ctx, stsMock, ec2Mock, "ap-south-1", "ami-0123456789abcdef0", "t3.micro")

		if !report.Passed {
			t.Errorf("expected overall preflight report to pass, but it failed")
		}
		if len(report.Checks) != 4 {
			t.Fatalf("expected 4 checks, got %d", len(report.Checks))
		}
		for _, c := range report.Checks {
			if !c.Passed {
				t.Errorf("check %s failed unexpectedly: %s", c.Name, c.Message)
			}
		}
	})

	t.Run("STS Authentication failure", func(t *testing.T) {
		stsMock := &mockSTSPreflightClient{
			err: errors.New("InvalidClientTokenId: The security token included in the request is invalid"),
		}
		ec2Mock := &mockEC2PreflightClient{}

		report := RunPreflightCheckWithClients(ctx, stsMock, ec2Mock, "ap-south-1", "ami-0123456789abcdef0", "t3.micro")

		if report.Passed {
			t.Errorf("expected overall report to fail due to STS error")
		}
		if report.Checks[0].Passed {
			t.Errorf("expected sts_identity check to fail")
		}
	})

	t.Run("Missing Base AMI failure", func(t *testing.T) {
		stsMock := &mockSTSPreflightClient{
			output: &sts.GetCallerIdentityOutput{
				Account: aws.String("123456789012"),
				Arn:     aws.String("arn:aws:iam::123456789012:user/admin"),
			},
		}
		ec2Mock := &mockEC2PreflightClient{
			vpcsOutput: &ec2.DescribeVpcsOutput{
				Vpcs: []ec2types.Vpc{{VpcId: aws.String("vpc-1"), IsDefault: aws.Bool(true)}},
			},
			subnetsOutput: &ec2.DescribeSubnetsOutput{
				Subnets: []ec2types.Subnet{{SubnetId: aws.String("subnet-1")}},
			},
			imagesOutput: &ec2.DescribeImagesOutput{
				Images: []ec2types.Image{}, // Not found
			},
			attrsOutput: &ec2.DescribeAccountAttributesOutput{
				AccountAttributes: []ec2types.AccountAttribute{},
			},
		}

		report := RunPreflightCheckWithClients(ctx, stsMock, ec2Mock, "ap-south-1", "ami-nonexistent", "t3.micro")

		if report.Passed {
			t.Errorf("expected report to fail when AMI is not found")
		}
		if report.Checks[2].Passed {
			t.Errorf("expected source_ami_exists check to fail")
		}
	})
}

func TestListNetworks(t *testing.T) {
	ctx := context.Background()

	ec2Mock := &mockEC2PreflightClient{
		vpcsOutput: &ec2.DescribeVpcsOutput{
			Vpcs: []ec2types.Vpc{
				{
					VpcId:     aws.String("vpc-custom123"),
					IsDefault: aws.Bool(false),
					CidrBlock: aws.String("10.0.0.0/16"),
					Tags: []ec2types.Tag{
						{Key: aws.String("Name"), Value: aws.String("Production-VPC")},
					},
				},
				{
					VpcId:     aws.String("vpc-default456"),
					IsDefault: aws.Bool(true),
					CidrBlock: aws.String("172.31.0.0/16"),
				},
			},
		},
		subnetsOutput: &ec2.DescribeSubnetsOutput{
			Subnets: []ec2types.Subnet{
				{
					SubnetId:                aws.String("subnet-bbb"),
					VpcId:                   aws.String("vpc-custom123"),
					AvailabilityZone:        aws.String("ap-south-1b"),
					MapPublicIpOnLaunch:     aws.Bool(false),
					AvailableIpAddressCount: aws.Int32(100),
					Tags: []ec2types.Tag{
						{Key: aws.String("Name"), Value: aws.String("Private-Subnet-1b")},
					},
				},
				{
					SubnetId:                aws.String("subnet-aaa"),
					VpcId:                   aws.String("vpc-default456"),
					AvailabilityZone:        aws.String("ap-south-1a"),
					MapPublicIpOnLaunch:     aws.Bool(true),
					AvailableIpAddressCount: aws.Int32(250),
				},
			},
		},
	}

	res, err := ListNetworksWithClient(ctx, ec2Mock, "ap-south-1")
	if err != nil {
		t.Fatalf("ListNetworksWithClient failed: %v", err)
	}

	if len(res.VPCs) != 2 {
		t.Fatalf("expected 2 VPCs, got %d", len(res.VPCs))
	}
	// Default VPC should sort first
	if !res.VPCs[0].IsDefault {
		t.Errorf("expected first VPC to be default, got %v", res.VPCs[0])
	}
	if res.VPCs[1].Name != "Production-VPC" {
		t.Errorf("expected second VPC name to be Production-VPC, got %q", res.VPCs[1].Name)
	}

	if len(res.Subnets) != 2 {
		t.Fatalf("expected 2 Subnets, got %d", len(res.Subnets))
	}
	// Sorted by AZ
	if res.Subnets[0].AvailabilityZone != "ap-south-1a" {
		t.Errorf("expected first subnet in ap-south-1a, got %q", res.Subnets[0].AvailabilityZone)
	}
}
