package aws

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/ec2"
	ec2types "github.com/aws/aws-sdk-go-v2/service/ec2/types"
)

type mockEC2Client struct {
	images map[string][]ec2types.Image
	err    error
}

func (m *mockEC2Client) DescribeImages(ctx context.Context, params *ec2.DescribeImagesInput, optFns ...func(*ec2.Options)) (*ec2.DescribeImagesOutput, error) {
	if m.err != nil {
		return nil, m.err
	}

	namePattern := ""
	for _, f := range params.Filters {
		if aws.ToString(f.Name) == "name" && len(f.Values) > 0 {
			namePattern = f.Values[0]
		}
	}

	res := m.images[namePattern]
	return &ec2.DescribeImagesOutput{
		Images: res,
	}, nil
}

func TestListOfficialBaseAMIsWithMockClient(t *testing.T) {
	mockClient := &mockEC2Client{
		images: map[string][]ec2types.Image{
			"ubuntu/images/hvm-ssd-gp3/ubuntu-noble-24.04-amd64-server-*": {
				{
					ImageId:      aws.String("ami-ubuntu24-old"),
					Name:         aws.String("ubuntu/images/hvm-ssd-gp3/ubuntu-noble-24.04-amd64-server-20240401"),
					CreationDate: aws.String("2024-04-01T12:00:00.000Z"),
				},
				{
					ImageId:      aws.String("ami-ubuntu24-newest"),
					Name:         aws.String("ubuntu/images/hvm-ssd-gp3/ubuntu-noble-24.04-amd64-server-20240425"),
					CreationDate: aws.String("2024-04-25T12:00:00.000Z"),
				},
			},
			"ubuntu/images/hvm-ssd/ubuntu-jammy-22.04-amd64-server-*": {
				{
					ImageId:      aws.String("ami-ubuntu22-latest"),
					Name:         aws.String("ubuntu/images/hvm-ssd/ubuntu-jammy-22.04-amd64-server-20240410"),
					CreationDate: aws.String("2024-04-10T12:00:00.000Z"),
				},
			},
			"al2023-ami-2023.*-kernel-6.1-x86_64": {
				{
					ImageId:      aws.String("ami-al2023-latest"),
					Name:         aws.String("al2023-ami-2023.4.20240416.0-kernel-6.1-x86_64"),
					CreationDate: aws.String("2024-04-16T12:00:00.000Z"),
				},
			},
			"debian-12-amd64-*": {
				{
					ImageId:      aws.String("ami-debian12-latest"),
					Name:         aws.String("debian-12-amd64-20240415-1718"),
					CreationDate: aws.String("2024-04-15T12:00:00.000Z"),
				},
			},
		},
	}

	ctx := context.Background()
	res, err := ListOfficialBaseAMIsWithClient(ctx, mockClient, "ap-south-1", "x86_64")
	if err != nil {
		t.Fatalf("ListOfficialBaseAMIsWithClient failed: %v", err)
	}

	if res.Source != "aws_live" {
		t.Errorf("expected source aws_live, got %q", res.Source)
	}

	if len(res.Images) != 4 {
		t.Fatalf("expected 4 images, got %d", len(res.Images))
	}

	// Verify Ubuntu 24 selected newest creation date (Packer most_recent = true)
	foundU24 := false
	for _, img := range res.Images {
		if img.OS == "ubuntu-24.04" {
			foundU24 = true
			if img.ID != "ami-ubuntu24-newest" {
				t.Errorf("expected ami-ubuntu24-newest, got %q", img.ID)
			}
			if img.DefaultUser != "ubuntu" {
				t.Errorf("expected default user ubuntu, got %q", img.DefaultUser)
			}
		} else if img.OS == "amazon-linux-2023" {
			if img.DefaultUser != "ec2-user" {
				t.Errorf("expected ec2-user, got %q", img.DefaultUser)
			}
		} else if img.OS == "debian-12" {
			if img.DefaultUser != "admin" {
				t.Errorf("expected admin, got %q", img.DefaultUser)
			}
		}
	}
	if !foundU24 {
		t.Errorf("ubuntu-24.04 not found in results")
	}
}

func TestUnauthenticatedReturnsCleanError(t *testing.T) {
	ClearAMICache()

	// When client returns an error (e.g. unauthenticated), it returns not_connected without hardcoded fallback AMIs
	mockClient := &mockEC2Client{
		err: errors.New("UnauthorizedOperation: You are not authorized to perform this operation"),
	}

	ctx := context.Background()
	_, err := ListOfficialBaseAMIsWithClient(ctx, mockClient, "us-east-1", "x86_64")
	if err == nil {
		t.Fatalf("expected error from client, got nil")
	}

	cfg := aws.Config{}
	res := ListOfficialBaseAMIs(ctx, cfg, "us-east-1", "x86_64")
	if res.Source != "not_connected" {
		t.Errorf("expected not_connected, got %q", res.Source)
	}
	if len(res.Images) != 0 {
		t.Errorf("expected 0 images when unauthenticated, got %d", len(res.Images))
	}
	if res.Error == "" {
		t.Errorf("expected error message when unauthenticated, got empty string")
	}
}

func TestCacheOperation(t *testing.T) {
	ClearAMICache()
	cfg := aws.Config{}

	// Manually inject cache entry to test cache hit
	amiCacheMu.Lock()
	amiCache["eu-west-1:x86_64"] = cacheEntry{
		response: BaseAMIResponse{
			Region: "eu-west-1",
			Arch:   "x86_64",
			Source: "aws_live",
			Images: []BaseAMI{{ID: "ami-cached-test"}},
		},
		expiresAt: time.Now().Add(10 * time.Minute),
	}
	amiCacheMu.Unlock()

	resCached := ListOfficialBaseAMIs(context.Background(), cfg, "eu-west-1", "x86_64")
	if resCached.Source != "aws_live" || len(resCached.Images) != 1 || resCached.Images[0].ID != "ami-cached-test" {
		t.Errorf("expected cache hit with ami-cached-test, got %+v", resCached)
	}
}
