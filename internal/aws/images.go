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

// BaseAMI represents a discovered or preset AWS official base machine image.
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
	Source string    `json:"source"` // "aws_live" or "fallback_preset"
	Images []BaseAMI `json:"images"`
}

// EC2DescribeImagesAPI abstracts the EC2 DescribeImages call for testing.
type EC2DescribeImagesAPI interface {
	DescribeImages(ctx context.Context, params *ec2.DescribeImagesInput, optFns ...func(*ec2.Options)) (*ec2.DescribeImagesOutput, error)
}

// AMI query specification
type amiQuerySpec struct {
	OS          string
	Family      string
	DisplayName string
	OwnerID     string
	NamePattern string
	DefaultUser string
	Description string
}

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
			OwnerID:     "099720109477",
			NamePattern: fmt.Sprintf("ubuntu/images/hvm-ssd-gp3/ubuntu-noble-24.04-%s-server-*", ubuntuArch),
			DefaultUser: "ubuntu",
			Description: "Canonical Ubuntu 24.04 LTS with Linux 6.8 kernel and systemd",
		},
		{
			OS:          "ubuntu-22.04",
			Family:      "Ubuntu",
			DisplayName: "Ubuntu 22.04 LTS (Jammy Jellyfish)",
			OwnerID:     "099720109477",
			NamePattern: fmt.Sprintf("ubuntu/images/hvm-ssd/ubuntu-jammy-22.04-%s-server-*", ubuntuArch),
			DefaultUser: "ubuntu",
			Description: "Canonical Ubuntu 22.04 LTS with Linux 5.15 kernel and systemd",
		},
		{
			OS:          "amazon-linux-2023",
			Family:      "Amazon Linux",
			DisplayName: "Amazon Linux 2023",
			OwnerID:     "137112412989",
			NamePattern: fmt.Sprintf("al2023-ami-2023.*-kernel-6.1-%s", arch),
			DefaultUser: "ec2-user",
			Description: "Amazon Linux 2023 optimized for AWS infrastructure",
		},
		{
			OS:          "debian-12",
			Family:      "Debian",
			DisplayName: "Debian 12 (Bookworm)",
			OwnerID:     "136693071363",
			NamePattern: fmt.Sprintf("debian-12-%s-*", ubuntuArch),
			DefaultUser: "admin",
			Description: "Official Debian 12 cloud image for AWS",
		},
	}
}

// In-memory 15-minute cache
type cacheEntry struct {
	response  BaseAMIResponse
	expiresAt time.Time
}

var (
	amiCache   = make(map[string]cacheEntry)
	amiCacheMu sync.RWMutex
	cacheTTL   = 15 * time.Minute
)

// ListOfficialBaseAMIs fetches the latest official AMIs for the given region and architecture.
// Uses in-memory cache, queries EC2 DescribeImages if connected, and falls back to curated presets.
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

	// Attempt live AWS EC2 query
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

	// Fallback to curated presets
	fallback := GetCuratedPresets(region, arch)
	return fallback
}

// ListOfficialBaseAMIsWithClient executes DescribeImages queries against the provided EC2 API.
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

		// Sort by CreationDate descending
		sort.Slice(output.Images, func(i, j int) bool {
			dateI := aws.ToString(output.Images[i].CreationDate)
			dateJ := aws.ToString(output.Images[j].CreationDate)
			return dateI > dateJ
		})

		latest := output.Images[0]
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
			OwnerID:      spec.OwnerID,
		})
	}

	if len(discovered) == 0 {
		return BaseAMIResponse{}, fmt.Errorf("no official AMIs discovered for %s %s", region, arch)
	}

	return BaseAMIResponse{
		Region: region,
		Arch:   arch,
		Source: "aws_live",
		Images: discovered,
	}, nil
}

// GetCuratedPresets returns high-confidence official AMIs for common AWS regions when offline or unauthenticated.
func GetCuratedPresets(region, arch string) BaseAMIResponse {
	if region == "" {
		region = "ap-south-1"
	}
	if arch == "" {
		arch = "x86_64"
	}

	presetsByRegion := map[string]map[string]string{
		"ap-south-1": {
			"ubuntu-24.04":      "ami-0dee22c13ea7a9a67",
			"ubuntu-22.04":      "ami-0f58b397bc5c1f2e8",
			"amazon-linux-2023": "ami-03f4878e83fedf440",
			"debian-12":         "ami-04149021e102213e4",
		},
		"us-east-1": {
			"ubuntu-24.04":      "ami-04b70fa74e45c3917",
			"ubuntu-22.04":      "ami-0c7217cdde317cfec",
			"amazon-linux-2023": "ami-0440d3b780d96b29d",
			"debian-12":         "ami-06db4d78cb1d3bbf9",
		},
		"us-west-2": {
			"ubuntu-24.04":      "ami-07d9cf938edb0739b",
			"ubuntu-22.04":      "ami-03f65b8614a860c29",
			"amazon-linux-2023": "ami-00448a337e32a6774",
			"debian-12":         "ami-08a52ddb321b32a8c",
		},
		"eu-west-1": {
			"ubuntu-24.04":      "ami-0c1c30f31d2ae7aa7",
			"ubuntu-22.04":      "ami-0694d931cee176e7d",
			"amazon-linux-2023": "ami-0c956e207f9d113d5",
			"debian-12":         "ami-07b1a20a77196a600",
		},
		"eu-central-1": {
			"ubuntu-24.04":      "ami-0084a47cc718c111a",
			"ubuntu-22.04":      "ami-04e601abe3e1a910f",
			"amazon-linux-2023": "ami-026c3177c9ad54336",
			"debian-12":         "ami-0782e4e1a72d73501",
		},
		"ap-southeast-1": {
			"ubuntu-24.04":      "ami-060e277c0d4cce553",
			"ubuntu-22.04":      "ami-0df7a207adb9748c7",
			"amazon-linux-2023": "ami-07a6f8f647e8f2713",
			"debian-12":         "ami-0329b3bb887964b49",
		},
	}

	regMap, exists := presetsByRegion[region]
	if !exists {
		// Fallback to ap-south-1 if region is unlisted
		regMap = presetsByRegion["ap-south-1"]
	}

	specs := getQuerySpecs(arch)
	images := make([]BaseAMI, 0, len(specs))

	for _, s := range specs {
		amiID := regMap[s.OS]
		if amiID == "" {
			amiID = "ami-0f58b397bc5c1f2e8"
		}

		images = append(images, BaseAMI{
			ID:           amiID,
			Name:         s.NamePattern,
			DisplayName:  s.DisplayName,
			OS:           s.OS,
			Family:       s.Family,
			Architecture: arch,
			DefaultUser:  s.DefaultUser,
			CreationDate: "2024-04-01T00:00:00.000Z",
			Description:  s.Description + " (curated fallback preset)",
			OwnerID:      s.OwnerID,
		})
	}

	return BaseAMIResponse{
		Region: region,
		Arch:   arch,
		Source: "fallback_preset",
		Images: images,
	}
}

// ClearAMICache clears the in-memory AMI cache.
func ClearAMICache() {
	amiCacheMu.Lock()
	defer amiCacheMu.Unlock()
	amiCache = make(map[string]cacheEntry)
}
