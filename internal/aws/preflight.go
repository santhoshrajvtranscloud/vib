package aws

import (
	"context"
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/ec2"
	ec2types "github.com/aws/aws-sdk-go-v2/service/ec2/types"
	"github.com/aws/aws-sdk-go-v2/service/sts"
)

// PreflightItem represents the result of an individual pre-flight verification step.
type PreflightItem struct {
	Name    string `json:"name"`  // "sts_identity", "network_ready", "source_ami_exists", "ec2_permissions"
	Title   string `json:"title"` // Human-readable title
	Passed  bool   `json:"passed"`
	Message string `json:"message"`
	Details string `json:"details,omitempty"`
}

// PreflightReport aggregates the 4-step AWS readiness check.
type PreflightReport struct {
	Region    string          `json:"region"`
	Passed    bool            `json:"passed"`
	Checks    []PreflightItem `json:"checks"`
	Timestamp time.Time       `json:"timestamp"`
}

// VPCInfo contains summary metadata for an Amazon VPC.
type VPCInfo struct {
	VPCID     string `json:"vpc_id"`
	Name      string `json:"name"`
	IsDefault bool   `json:"is_default"`
	CIDR      string `json:"cidr"`
}

// SubnetInfo contains summary metadata for an Amazon EC2 Subnet.
type SubnetInfo struct {
	SubnetID         string `json:"subnet_id"`
	VPCID            string `json:"vpc_id"`
	AvailabilityZone string `json:"availability_zone"`
	Name             string `json:"name"`
	PublicIPOnLaunch bool   `json:"public_ip_on_launch"`
	AvailableIPCount int32  `json:"available_ip_count,omitempty"`
}

// NetworksResponse aggregates available VPCs and Subnets in a region.
type NetworksResponse struct {
	Region  string       `json:"region"`
	VPCs    []VPCInfo    `json:"vpcs"`
	Subnets []SubnetInfo `json:"subnets"`
}

// EC2PreflightAPI abstracts EC2 operations needed for preflight checks and network discovery.
type EC2PreflightAPI interface {
	DescribeVpcs(ctx context.Context, params *ec2.DescribeVpcsInput, optFns ...func(*ec2.Options)) (*ec2.DescribeVpcsOutput, error)
	DescribeSubnets(ctx context.Context, params *ec2.DescribeSubnetsInput, optFns ...func(*ec2.Options)) (*ec2.DescribeSubnetsOutput, error)
	DescribeImages(ctx context.Context, params *ec2.DescribeImagesInput, optFns ...func(*ec2.Options)) (*ec2.DescribeImagesOutput, error)
	DescribeAccountAttributes(ctx context.Context, params *ec2.DescribeAccountAttributesInput, optFns ...func(*ec2.Options)) (*ec2.DescribeAccountAttributesOutput, error)
}

// STSPreflightAPI abstracts STS identity calls for preflight checks.
type STSPreflightAPI interface {
	GetCallerIdentity(ctx context.Context, params *sts.GetCallerIdentityInput, optFns ...func(*sts.Options)) (*sts.GetCallerIdentityOutput, error)
}

// RunPreflightCheck executes the 4-step readiness diagnostic against AWS APIs.
func RunPreflightCheck(ctx context.Context, cfg aws.Config, region, sourceAMI, instanceType string) PreflightReport {
	if region == "" {
		region = "ap-south-1"
	}

	stsClient := sts.NewFromConfig(cfg)
	ec2Client := ec2.NewFromConfig(cfg, func(o *ec2.Options) {
		o.Region = region
	})

	return RunPreflightCheckWithClients(ctx, stsClient, ec2Client, region, sourceAMI, instanceType)
}

// RunPreflightCheckWithClients allows testing with mock clients.
func RunPreflightCheckWithClients(ctx context.Context, stsClient STSPreflightAPI, ec2Client EC2PreflightAPI, region, sourceAMI, instanceType string) PreflightReport {
	checks := make([]PreflightItem, 0, 4)
	overallPassed := true

	// Check 1: STS Caller Identity
	stsItem := PreflightItem{
		Name:  "sts_identity",
		Title: "AWS STS Identity & Credentials",
	}
	identity, err := stsClient.GetCallerIdentity(ctx, &sts.GetCallerIdentityInput{})
	if err != nil {
		stsItem.Passed = false
		stsItem.Message = fmt.Sprintf("Authentication failed: %v", err)
		overallPassed = false
	} else {
		stsItem.Passed = true
		stsItem.Message = fmt.Sprintf("Authenticated as %s (Account: %s)", aws.ToString(identity.Arn), aws.ToString(identity.Account))
		stsItem.Details = aws.ToString(identity.UserId)
	}
	checks = append(checks, stsItem)

	// Check 2: Network Readiness (VPC and Subnet Availability)
	netItem := PreflightItem{
		Name:  "network_ready",
		Title: "VPC & Subnet Readiness",
	}
	vpcsOut, errVpc := ec2Client.DescribeVpcs(ctx, &ec2.DescribeVpcsInput{})
	subnetsOut, errSubnet := ec2Client.DescribeSubnets(ctx, &ec2.DescribeSubnetsInput{})
	if errVpc != nil || vpcsOut == nil {
		netItem.Passed = false
		if errVpc != nil {
			netItem.Message = fmt.Sprintf("Failed to query VPCs: %v", errVpc)
		} else {
			netItem.Message = "No VPC response from AWS EC2"
		}
		overallPassed = false
	} else if errSubnet != nil || subnetsOut == nil {
		netItem.Passed = false
		if errSubnet != nil {
			netItem.Message = fmt.Sprintf("Failed to query Subnets: %v", errSubnet)
		} else {
			netItem.Message = "No Subnet response from AWS EC2"
		}
		overallPassed = false
	} else {
		hasDefault := false
		defaultVpcID := ""
		for _, v := range vpcsOut.Vpcs {
			if aws.ToBool(v.IsDefault) {
				hasDefault = true
				defaultVpcID = aws.ToString(v.VpcId)
				break
			}
		}

		totalSubnets := len(subnetsOut.Subnets)
		if hasDefault {
			netItem.Passed = true
			netItem.Message = fmt.Sprintf("Default VPC %s ready with %d subnets in %s", defaultVpcID, totalSubnets, region)
		} else if len(vpcsOut.Vpcs) > 0 && totalSubnets > 0 {
			netItem.Passed = true
			firstVPC := aws.ToString(vpcsOut.Vpcs[0].VpcId)
			netItem.Message = fmt.Sprintf("%d custom VPCs and %d subnets available (primary: %s)", len(vpcsOut.Vpcs), totalSubnets, firstVPC)
		} else {
			netItem.Passed = false
			netItem.Message = fmt.Sprintf("No default VPC or usable subnets found in %s", region)
			overallPassed = false
		}
	}
	checks = append(checks, netItem)

	// Check 3: Base AMI Existence & State
	amiItem := PreflightItem{
		Name:  "source_ami_exists",
		Title: "Source Base AMI Verification",
	}
	if strings.TrimSpace(sourceAMI) == "" {
		amiItem.Passed = false
		amiItem.Message = "Source AMI ID is not specified"
		overallPassed = false
	} else {
		imgOut, errImg := ec2Client.DescribeImages(ctx, &ec2.DescribeImagesInput{
			ImageIds: []string{strings.TrimSpace(sourceAMI)},
		})
		if errImg != nil || imgOut == nil {
			amiItem.Passed = false
			if errImg != nil {
				amiItem.Message = fmt.Sprintf("Base AMI %s not found in %s: %v", sourceAMI, region, errImg)
			} else {
				amiItem.Message = fmt.Sprintf("Base AMI %s not found in region %s", sourceAMI, region)
			}
			overallPassed = false
		} else if len(imgOut.Images) == 0 {
			amiItem.Passed = false
			amiItem.Message = fmt.Sprintf("Base AMI %s does not exist in %s", sourceAMI, region)
			overallPassed = false
		} else {
			img := imgOut.Images[0]
			if img.State == ec2types.ImageStateAvailable {
				amiItem.Passed = true
				amiItem.Message = fmt.Sprintf("Base AMI %s is available (%s, %s)", sourceAMI, aws.ToString(img.Name), string(img.Architecture))
			} else {
				amiItem.Passed = false
				amiItem.Message = fmt.Sprintf("Base AMI %s is in state %q (expected 'available')", sourceAMI, string(img.State))
				overallPassed = false
			}
		}
	}
	checks = append(checks, amiItem)

	// Check 4: EC2 API Permissions
	permItem := PreflightItem{
		Name:  "ec2_permissions",
		Title: "EC2 API Operations & Permissions",
	}
	attrOut, errAttr := ec2Client.DescribeAccountAttributes(ctx, &ec2.DescribeAccountAttributesInput{})
	if errAttr != nil || attrOut == nil {
		permItem.Passed = false
		if errAttr != nil {
			permItem.Message = fmt.Sprintf("EC2 API access denied or limited: %v", errAttr)
		} else {
			permItem.Message = "No EC2 account attributes returned"
		}
		overallPassed = false
	} else {
		permItem.Passed = true
		permItem.Message = fmt.Sprintf("EC2 API access verified (%d account attributes readable)", len(attrOut.AccountAttributes))
	}
	checks = append(checks, permItem)

	return PreflightReport{
		Region:    region,
		Passed:    overallPassed,
		Checks:    checks,
		Timestamp: time.Now().UTC(),
	}
}

// ListNetworks queries and returns all VPCs and Subnets in the specified region.
func ListNetworks(ctx context.Context, cfg aws.Config, region string) (*NetworksResponse, error) {
	if region == "" {
		region = "ap-south-1"
	}

	ec2Client := ec2.NewFromConfig(cfg, func(o *ec2.Options) {
		o.Region = region
	})

	return ListNetworksWithClient(ctx, ec2Client, region)
}

// ListNetworksWithClient extracts VPCs and Subnets using an injected EC2 client.
func ListNetworksWithClient(ctx context.Context, ec2Client EC2PreflightAPI, region string) (*NetworksResponse, error) {
	vpcsOut, err := ec2Client.DescribeVpcs(ctx, &ec2.DescribeVpcsInput{})
	if err != nil {
		return nil, fmt.Errorf("failed to describe VPCs: %w", err)
	}

	subnetsOut, err := ec2Client.DescribeSubnets(ctx, &ec2.DescribeSubnetsInput{})
	if err != nil {
		return nil, fmt.Errorf("failed to describe Subnets: %w", err)
	}

	vpcs := make([]VPCInfo, 0, len(vpcsOut.Vpcs))
	for _, v := range vpcsOut.Vpcs {
		name := aws.ToString(v.VpcId)
		for _, tag := range v.Tags {
			if aws.ToString(tag.Key) == "Name" && strings.TrimSpace(aws.ToString(tag.Value)) != "" {
				name = aws.ToString(tag.Value)
				break
			}
		}

		vpcs = append(vpcs, VPCInfo{
			VPCID:     aws.ToString(v.VpcId),
			Name:      name,
			IsDefault: aws.ToBool(v.IsDefault),
			CIDR:      aws.ToString(v.CidrBlock),
		})
	}

	// Sort default VPC to top, then alphabetical by name
	sort.Slice(vpcs, func(i, j int) bool {
		if vpcs[i].IsDefault != vpcs[j].IsDefault {
			return vpcs[i].IsDefault
		}
		return vpcs[i].Name < vpcs[j].Name
	})

	subnets := make([]SubnetInfo, 0, len(subnetsOut.Subnets))
	for _, s := range subnetsOut.Subnets {
		name := aws.ToString(s.SubnetId)
		for _, tag := range s.Tags {
			if aws.ToString(tag.Key) == "Name" && strings.TrimSpace(aws.ToString(tag.Value)) != "" {
				name = aws.ToString(tag.Value)
				break
			}
		}

		subnets = append(subnets, SubnetInfo{
			SubnetID:         aws.ToString(s.SubnetId),
			VPCID:            aws.ToString(s.VpcId),
			AvailabilityZone: aws.ToString(s.AvailabilityZone),
			Name:             name,
			PublicIPOnLaunch: aws.ToBool(s.MapPublicIpOnLaunch),
			AvailableIPCount: aws.ToInt32(s.AvailableIpAddressCount),
		})
	}

	// Sort subnets by Availability Zone, then name
	sort.Slice(subnets, func(i, j int) bool {
		if subnets[i].AvailabilityZone != subnets[j].AvailabilityZone {
			return subnets[i].AvailabilityZone < subnets[j].AvailabilityZone
		}
		return subnets[i].Name < subnets[j].Name
	})

	return &NetworksResponse{
		Region:  region,
		VPCs:    vpcs,
		Subnets: subnets,
	}, nil
}
