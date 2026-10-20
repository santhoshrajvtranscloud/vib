package aws

import (
	"context"
	"fmt"
	"testing"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/ec2"
	ec2types "github.com/aws/aws-sdk-go-v2/service/ec2/types"
)

type mockEC2Lifecycle struct {
	images             []ec2types.Image
	deregisteredAMIs   []string
	deletedSnapshotIDs []string
	describeErr        error
	deregisterErr      error
	deleteSnapshotErr  error
}

func (m *mockEC2Lifecycle) DescribeImages(ctx context.Context, params *ec2.DescribeImagesInput, optFns ...func(*ec2.Options)) (*ec2.DescribeImagesOutput, error) {
	if m.describeErr != nil {
		return nil, m.describeErr
	}
	if len(params.ImageIds) > 0 {
		var matched []ec2types.Image
		for _, img := range m.images {
			for _, id := range params.ImageIds {
				if aws.ToString(img.ImageId) == id {
					matched = append(matched, img)
				}
			}
		}
		return &ec2.DescribeImagesOutput{Images: matched}, nil
	}
	return &ec2.DescribeImagesOutput{Images: m.images}, nil
}

func (m *mockEC2Lifecycle) DeregisterImage(ctx context.Context, params *ec2.DeregisterImageInput, optFns ...func(*ec2.Options)) (*ec2.DeregisterImageOutput, error) {
	if m.deregisterErr != nil {
		return nil, m.deregisterErr
	}
	m.deregisteredAMIs = append(m.deregisteredAMIs, aws.ToString(params.ImageId))
	return &ec2.DeregisterImageOutput{}, nil
}

func (m *mockEC2Lifecycle) DeleteSnapshot(ctx context.Context, params *ec2.DeleteSnapshotInput, optFns ...func(*ec2.Options)) (*ec2.DeleteSnapshotOutput, error) {
	if m.deleteSnapshotErr != nil {
		return nil, m.deleteSnapshotErr
	}
	m.deletedSnapshotIDs = append(m.deletedSnapshotIDs, aws.ToString(params.SnapshotId))
	return &ec2.DeleteSnapshotOutput{}, nil
}

func TestListVIBImages(t *testing.T) {
	mock := &mockEC2Lifecycle{
		images: []ec2types.Image{
			{
				ImageId:      aws.String("ami-11111111111111111"),
				Name:         aws.String("vib-app-1"),
				CreationDate: aws.String("2024-04-01T12:00:00.000Z"),
				State:        ec2types.ImageStateAvailable,
				Tags: []ec2types.Tag{
					{Key: aws.String("CreatedBy"), Value: aws.String("VIB")},
					{Key: aws.String("VIB_Build_ID"), Value: aws.String("vib-build-1")},
				},
				BlockDeviceMappings: []ec2types.BlockDeviceMapping{
					{
						Ebs: &ec2types.EbsBlockDevice{
							SnapshotId: aws.String("snap-11111"),
						},
					},
				},
			},
			{
				ImageId:      aws.String("ami-22222222222222222"),
				Name:         aws.String("vib-app-2"),
				CreationDate: aws.String("2024-04-02T12:00:00.000Z"),
				State:        ec2types.ImageStateAvailable,
				Tags: []ec2types.Tag{
					{Key: aws.String("CreatedBy"), Value: aws.String("VIB")},
					{Key: aws.String("VIB_Build_ID"), Value: aws.String("vib-build-2")},
				},
				BlockDeviceMappings: []ec2types.BlockDeviceMapping{
					{
						Ebs: &ec2types.EbsBlockDevice{
							SnapshotId: aws.String("snap-22222"),
						},
					},
					{
						Ebs: &ec2types.EbsBlockDevice{
							SnapshotId: aws.String("snap-33333"),
						},
					},
				},
			},
		},
	}

	images, err := ListVIBImagesWithClient(context.Background(), mock, "ap-south-1")
	if err != nil {
		t.Fatalf("ListVIBImagesWithClient failed: %v", err)
	}

	if len(images) != 2 {
		t.Fatalf("expected 2 images, got %d", len(images))
	}

	// Should be sorted newest first (ami-2222... is 2024-04-02)
	if images[0].AMIID != "ami-22222222222222222" {
		t.Errorf("expected newest image first, got %s", images[0].AMIID)
	}
	if images[0].BuildID != "vib-build-2" {
		t.Errorf("expected buildID vib-build-2, got %s", images[0].BuildID)
	}
	if len(images[0].SnapshotIDs) != 2 {
		t.Errorf("expected 2 snapshots, got %d", len(images[0].SnapshotIDs))
	}
}

func TestDeleteVIBImage(t *testing.T) {
	mock := &mockEC2Lifecycle{
		images: []ec2types.Image{
			{
				ImageId: aws.String("ami-1234567890abcdef0"),
				Name:    aws.String("vib-test-ami"),
				BlockDeviceMappings: []ec2types.BlockDeviceMapping{
					{
						Ebs: &ec2types.EbsBlockDevice{
							SnapshotId: aws.String("snap-aaa111"),
						},
					},
					{
						Ebs: &ec2types.EbsBlockDevice{
							SnapshotId: aws.String("snap-bbb222"),
						},
					},
				},
			},
		},
	}

	result, err := DeleteVIBImageWithClient(context.Background(), mock, "ami-1234567890abcdef0")
	if err != nil {
		t.Fatalf("DeleteVIBImageWithClient failed: %v", err)
	}

	if !result.Deregistered {
		t.Errorf("expected Deregistered to be true")
	}
	if len(result.DeletedSnapshotIDs) != 2 {
		t.Fatalf("expected 2 deleted snapshots, got %d", len(result.DeletedSnapshotIDs))
	}
	if len(mock.deregisteredAMIs) != 1 || mock.deregisteredAMIs[0] != "ami-1234567890abcdef0" {
		t.Errorf("expected AMI deregistered: %v", mock.deregisteredAMIs)
	}
	if len(mock.deletedSnapshotIDs) != 2 {
		t.Errorf("expected 2 snapshot delete calls, got %d", len(mock.deletedSnapshotIDs))
	}
}

func TestDeleteVIBImageNotFound(t *testing.T) {
	mock := &mockEC2Lifecycle{
		images: []ec2types.Image{},
	}

	_, err := DeleteVIBImageWithClient(context.Background(), mock, "ami-nonexistent")
	if err == nil {
		t.Errorf("expected error when AMI not found, got nil")
	}
}

func TestDeleteVIBImageSnapshotError(t *testing.T) {
	mock := &mockEC2Lifecycle{
		images: []ec2types.Image{
			{
				ImageId: aws.String("ami-test"),
				BlockDeviceMappings: []ec2types.BlockDeviceMapping{
					{
						Ebs: &ec2types.EbsBlockDevice{
							SnapshotId: aws.String("snap-err"),
						},
					},
				},
			},
		},
		deleteSnapshotErr: fmt.Errorf("insufficient permissions to delete snapshot"),
	}

	result, err := DeleteVIBImageWithClient(context.Background(), mock, "ami-test")
	if err == nil {
		t.Errorf("expected error when snapshot deletion fails, got nil")
	}
	if !result.Deregistered {
		t.Errorf("expected AMI to be deregistered despite snapshot error")
	}
}
