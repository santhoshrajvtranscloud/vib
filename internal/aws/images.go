package aws

import (
	"context"
	"fmt"
	"sort"
	"sync"
	"time"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/ec2"
	ec2types "github.com/aws/aws-sdk-go-v2/service/ec2/types"
)

// BaseAMI represents an official base machine image discovered live from AWS EC2.
type BaseAMI struct {
	ID           string `json:"id"`
	Name         string `json:"name"`
	DisplayName  string `json:"display_name"`
	OS           string `json:"os"` // "ubuntu-24.04", "ubuntu-22.04", "amazon-linux-2023", "debian-12"
	Family       string `json:"family"`
	Architecture string `json:"architecture"` // "x86_64" or "arm64"
	DefaultUser  string `json:"default_user"`
	CreationDate string `json:"creation_date,omitempty"`
	Description  string `json:"description"`
	OwnerID      string `json:"owner_id"`
}

// BaseAMIResponse contains the response format for base AMI discovery.
type BaseAMIResponse struct {
	Region string    `json:"region"`
	Arch   string    `json:"arch"`
	Source string    `json:"source"` // "aws_live" or "not_connected"
	Images []BaseAMI `json:"images"`
	Error  string    `json:"error,omitempty"`
}

// EC2DescribeImagesAPI abstracts the EC2 DescribeImages call for testing and dependency injection.
type EC2DescribeImagesAPI interface {
	DescribeImages(ctx context.Context, params *ec2.DescribeImagesInput, optFns ...func(*ec2.Options)) (*ec2.DescribeImagesOutput, error)
}

// amiQuerySpec models the exact query filters used by Packer's source_ami_filter block.
type amiQuerySpec struct {
	OS          string
	Family      string
	DisplayName string
	OwnerID     string
	NamePattern string
	DefaultUser string
	Description string
}

// getQuerySpecs defines official vendor account IDs and release name patterns.
// These match the official patterns used by HashiCorp Packer and Terraform.
func getQuerySpecs(arch string) []amiQuerySpec {
	if arch != "arm64" {
		arch = "x86_64"
	}

	ubuntuArch := "amd64"
	if arch == "arm64" {
		ubuntuArch = "arm64"
	}

	return []amiQuerySpec{
		{
			OS:          "ubuntu-24.04",
			Family:      "Ubuntu",
			DisplayName: "Ubuntu 24.04 LTS (Noble Numbat)",
			OwnerID:     "099720109477", // Canonical official AWS account
			NamePattern: fmt.Sprintf("ubuntu/images/hvm-ssd-gp3/ubuntu-noble-24.04-%s-server-*", ubuntuArch),
			DefaultUser: "ubuntu",
			Description: "Canonical Ubuntu 24.04 LTS with Linux 6.8 kernel and systemd",
		},
		{
			OS:          "ubuntu-22.04",
			Family:      "Ubuntu",
			DisplayName: "Ubuntu 22.04 LTS (Jammy Jellyfish)",
			OwnerID:     "099720109477", // Canonical official AWS account
			NamePattern: fmt.Sprintf("ubuntu/images/hvm-ssd/ubuntu-jammy-22.04-%s-server-*", ubuntuArch),
			DefaultUser: "ubuntu",
			Description: "Canonical Ubuntu 22.04 LTS with Linux 5.15 kernel and systemd",
		},
		{
			OS:          "amazon-linux-2023",
			Family:      "Amazon Linux",
			DisplayName: "Amazon Linux 2023",
			OwnerID:     "137112412989", // Amazon official AWS account
			NamePattern: fmt.Sprintf("al2023-ami-2023.*-kernel-6.1-%s", arch),
			DefaultUser: "ec2-user",
			Description: "Amazon Linux 2023 optimized for AWS EC2 infrastructure",
		},
		{
			OS:          "debian-12",
			Family:      "Debian",
			DisplayName: "Debian 12 (Bookworm)",
			OwnerID:     "136693071363", // Debian official AWS account
			NamePattern: fmt.Sprintf("debian-12-%s-*", ubuntuArch),
			DefaultUser: "admin",
			Description: "Official Debian 12 cloud image for AWS",
		},
	}
}

// In-memory 15-minute cache to avoid rate-limiting DescribeImages on rapid UI navigation
type cacheEntry struct {
	response  BaseAMIResponse
	expiresAt time.Time
}

var (
	amiCache   = make(map[string]cacheEntry)
	amiCacheMu sync.RWMutex
	cacheTTL   = 15 * time.Minute
)

// ListOfficialBaseAMIs queries AWS EC2 DescribeImages live using the Packer source_ami_filter method.
// Results are cached for 15 minutes per region/architecture.
// If unauthenticated or AWS returns an error, it returns an explicit error response with zero hardcoded IDs.
func ListOfficialBaseAMIs(ctx context.Context, cfg aws.Config, region, arch string) BaseAMIResponse {
	if region == "" {
		region = "ap-south-1"
	}
	if arch == "" {
		arch = "x86_64"
	}

	cacheKey := fmt.Sprintf("%s:%s", region, arch)
	amiCacheMu.RLock()
	if entry, found := amiCache[cacheKey]; found && time.Now().Before(entry.expiresAt) {
		amiCacheMu.RUnlock()
		return entry.response
	}
	amiCacheMu.RUnlock()

	// Query live AWS EC2 via official AWS SDK v2
	client := ec2.NewFromConfig(cfg)
	res, err := ListOfficialBaseAMIsWithClient(ctx, client, region, arch)
	if err == nil && len(res.Images) > 0 {
		res.Source = "aws_live"
		amiCacheMu.Lock()
		amiCache[cacheKey] = cacheEntry{
			response:  res,
			expiresAt: time.Now().Add(cacheTTL),
		}
		amiCacheMu.Unlock()
		return res
	}

	errMsg := "AWS credentials not configured or EC2 DescribeImages failed"
	if err != nil {
		errMsg = err.Error()
	}

	return BaseAMIResponse{
		Region: region,
		Arch:   arch,
		Source: "not_connected",
		Images: []BaseAMI{},
		Error:  errMsg,
	}
}

// ListOfficialBaseAMIsWithClient executes DescribeImages queries using the exact same filters
// and sorting mechanism that HashiCorp Packer's source_ami_filter builder uses:
// - Owners: Official vendor account IDs
// - Filters:
//   - name: Glob pattern (e.g., ubuntu-noble-24.04-amd64-server-*)
//   - root-device-type: ebs
//   - virtualization-type: hvm
//   - state: available
// - Most Recent: Sort by CreationDate descending to select the latest release
func ListOfficialBaseAMIsWithClient(ctx context.Context, client EC2DescribeImagesAPI, region, arch string) (BaseAMIResponse, error) {
	specs := getQuerySpecs(arch)
	var discovered []BaseAMI

	for _, spec := range specs {
		input := &ec2.DescribeImagesInput{
			Owners: []string{spec.OwnerID},
			Filters: []ec2types.Filter{
				{
					Name:   aws.String("name"),
					Values: []string{spec.NamePattern},
				},
				{
					Name:   aws.String("root-device-type"),
					Values: []string{"ebs"},
				},
				{
					Name:   aws.String("virtualization-type"),
					Values: []string{"hvm"},
				},
				{
					Name:   aws.String("state"),
					Values: []string{"available"},
				},
			},
		}

		output, err := client.DescribeImages(ctx, input)
		if err != nil {
			return BaseAMIResponse{}, err
		}

		if len(output.Images) == 0 {
			continue
		}

		// Sort by CreationDate descending (identical to Packer's most_recent = true)
		sort.Slice(output.Images, func(i, j int) bool {
			dateI := aws.ToString(output.Images[i].CreationDate)
			dateJ := aws.ToString(output.Images[j].CreationDate)
			return dateI > dateJ
		})

		latest := output.Images[0]
		ownerID := aws.ToString(latest.OwnerId)
		if ownerID == "" {
			ownerID = spec.OwnerID
		}
		discovered = append(discovered, BaseAMI{
			ID:           aws.ToString(latest.ImageId),
			Name:         aws.ToString(latest.Name),
			DisplayName:  spec.DisplayName,
			OS:           spec.OS,
			Family:       spec.Family,
			Architecture: arch,
			DefaultUser:  spec.DefaultUser,
			CreationDate: aws.ToString(latest.CreationDate),
			Description:  spec.Description,
			OwnerID:      ownerID,
		})
	}

	if len(discovered) == 0 {
		return BaseAMIResponse{}, fmt.Errorf("no official AMIs found in %s matching Packer filters for %s", region, arch)
	}

	return BaseAMIResponse{
		Region: region,
		Arch:   arch,
		Source: "aws_live",
		Images: discovered,
	}, nil
}

// ClearAMICache clears the in-memory AMI cache.
func ClearAMICache() {
	amiCacheMu.Lock()
	defer amiCacheMu.Unlock()
	amiCache = make(map[string]cacheEntry)
}
