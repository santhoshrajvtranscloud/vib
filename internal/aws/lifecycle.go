package aws

import (
	"context"
	"errors"
	"fmt"
	"sort"
	"strings"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/ec2"
	ec2types "github.com/aws/aws-sdk-go-v2/service/ec2/types"
)

// VIBImage describes an AMI created by VIB and discovered in AWS EC2.
type VIBImage struct {
	AMIID        string            `json:"ami_id"`
	Name         string            `json:"name"`
	Region       string            `json:"region"`
	CreationDate string            `json:"creation_date"`
	State        string            `json:"state"`
	Description  string            `json:"description"`
	SnapshotIDs  []string          `json:"snapshot_ids"`
	Tags         map[string]string `json:"tags"`
	BuildID      string            `json:"build_id,omitempty"`
}

// DeleteVIBImageResult details the resources cleaned up during AMI deletion.
type DeleteVIBImageResult struct {
	AMIID              string   `json:"ami_id"`
	Deregistered       bool     `json:"deregistered"`
	DeletedSnapshotIDs []string `json:"deleted_snapshot_ids"`
}

// EC2LifecycleAPI defines the subset of EC2 actions required for AMI management.
type EC2LifecycleAPI interface {
	DescribeImages(ctx context.Context, params *ec2.DescribeImagesInput, optFns ...func(*ec2.Options)) (*ec2.DescribeImagesOutput, error)
	DeregisterImage(ctx context.Context, params *ec2.DeregisterImageInput, optFns ...func(*ec2.Options)) (*ec2.DeregisterImageOutput, error)
	DeleteSnapshot(ctx context.Context, params *ec2.DeleteSnapshotInput, optFns ...func(*ec2.Options)) (*ec2.DeleteSnapshotOutput, error)
}

// ListVIBImages queries EC2 DescribeImages in the specified region filtered by tag CreatedBy=VIB.
func ListVIBImages(ctx context.Context, cfg aws.Config, region string) ([]VIBImage, error) {
	if region != "" {
		cfg.Region = region
	}
	client := ec2.NewFromConfig(cfg)
	return ListVIBImagesWithClient(ctx, client, cfg.Region)
}

// ListVIBImagesWithClient executes DescribeImages using the provided EC2 API client.
func ListVIBImagesWithClient(ctx context.Context, client EC2LifecycleAPI, region string) ([]VIBImage, error) {
	input := &ec2.DescribeImagesInput{
		Filters: []ec2types.Filter{
			{
				Name:   aws.String("tag:CreatedBy"),
				Values: []string{"VIB"},
			},
		},
	}

	output, err := client.DescribeImages(ctx, input)
	if err != nil {
		return nil, fmt.Errorf("failed to query VIB AMIs in EC2: %w", err)
	}

	var images []VIBImage
	for _, img := range output.Images {
		amiID := aws.ToString(img.ImageId)
		var snapIDs []string
		for _, bdm := range img.BlockDeviceMappings {
			if bdm.Ebs != nil && bdm.Ebs.SnapshotId != nil {
				snapIDs = append(snapIDs, aws.ToString(bdm.Ebs.SnapshotId))
			}
		}

		tags := make(map[string]string)
		buildID := ""
		for _, t := range img.Tags {
			k := aws.ToString(t.Key)
			v := aws.ToString(t.Value)
			tags[k] = v
			if k == "VIB_Build_ID" {
				buildID = v
			}
		}

		images = append(images, VIBImage{
			AMIID:        amiID,
			Name:         aws.ToString(img.Name),
			Region:       region,
			CreationDate: aws.ToString(img.CreationDate),
			State:        string(img.State),
			Description:  aws.ToString(img.Description),
			SnapshotIDs:  snapIDs,
			Tags:         tags,
			BuildID:      buildID,
		})
	}

	// Sort newest first
	sort.Slice(images, func(i, j int) bool {
		return images[i].CreationDate > images[j].CreationDate
	})

	return images, nil
}

// DeleteVIBImage cleanly deregisters the AMI and deletes all associated EBS snapshots.
func DeleteVIBImage(ctx context.Context, cfg aws.Config, region, amiID string) (*DeleteVIBImageResult, error) {
	if region != "" {
		cfg.Region = region
	}
	client := ec2.NewFromConfig(cfg)
	return DeleteVIBImageWithClient(ctx, client, amiID)
}

// DeleteVIBImageWithClient executes the three-step cleanup:
// 1. DescribeImages to retrieve SnapshotIDs
// 2. DeregisterImage to unregister the AMI
// 3. DeleteSnapshot for each EBS snapshot
func DeleteVIBImageWithClient(ctx context.Context, client EC2LifecycleAPI, amiID string) (*DeleteVIBImageResult, error) {
	amiID = strings.TrimSpace(amiID)
	if amiID == "" {
		return nil, errors.New("ami_id is required")
	}

	// Step 1: Identify all associated EBS snapshots
	describeInput := &ec2.DescribeImagesInput{
		ImageIds: []string{amiID},
	}
	output, err := client.DescribeImages(ctx, describeInput)
	if err != nil {
		return nil, fmt.Errorf("failed to describe AMI %q: %w", amiID, err)
	}

	if len(output.Images) == 0 {
		return nil, fmt.Errorf("AMI %q not found in EC2", amiID)
	}

	img := output.Images[0]
	var snapshotIDs []string
	for _, bdm := range img.BlockDeviceMappings {
		if bdm.Ebs != nil && bdm.Ebs.SnapshotId != nil {
			snapshotIDs = append(snapshotIDs, aws.ToString(bdm.Ebs.SnapshotId))
		}
	}

	// Step 2: Deregister Image
	deregInput := &ec2.DeregisterImageInput{
		ImageId: aws.String(amiID),
	}
	if _, err := client.DeregisterImage(ctx, deregInput); err != nil {
		return nil, fmt.Errorf("failed to deregister AMI %q: %w", amiID, err)
	}

	// Step 3: Delete each associated EBS snapshot
	var deletedSnapshots []string
	var snapErrors []string
	for _, snapID := range snapshotIDs {
		if snapID == "" {
			continue
		}
		snapInput := &ec2.DeleteSnapshotInput{
			SnapshotId: aws.String(snapID),
		}
		if _, err := client.DeleteSnapshot(ctx, snapInput); err != nil {
			snapErrors = append(snapErrors, fmt.Sprintf("failed to delete snapshot %q: %v", snapID, err))
		} else {
			deletedSnapshots = append(deletedSnapshots, snapID)
		}
	}

	if len(snapErrors) > 0 {
		return &DeleteVIBImageResult{
			AMIID:              amiID,
			Deregistered:       true,
			DeletedSnapshotIDs: deletedSnapshots,
		}, fmt.Errorf("AMI deregistered but errors occurred deleting snapshots: %s", strings.Join(snapErrors, "; "))
	}

	return &DeleteVIBImageResult{
		AMIID:              amiID,
		Deregistered:       true,
		DeletedSnapshotIDs: deletedSnapshots,
	}, nil
}
