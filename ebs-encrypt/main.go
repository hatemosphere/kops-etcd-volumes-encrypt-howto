package main

import (
	"flag"
	"fmt"
	"os"

	"github.com/aws/aws-sdk-go/aws"
	"github.com/aws/aws-sdk-go/aws/awserr"
	"github.com/aws/aws-sdk-go/aws/session"
	"github.com/aws/aws-sdk-go/service/ec2"
)

const (
	requiredVolumeState = "available"
	defaultKmsKeyID     = "alias/aws/ebs"
)

func createService() (*session.Session, *ec2.EC2) {
	fmt.Println("Creating AWS client session...")
	session, sessionErr := session.NewSession()
	if sessionErr != nil {
		fmt.Println("Could not create session", sessionErr)
		os.Exit(1)
	}

	fmt.Println("Creating EC2 service...")
	service := ec2.New(session)
	return session, service
}

func getVolumeMetadata(volumeID string, service *ec2.EC2) *ec2.DescribeVolumesOutput {
	fmt.Println("Getting " + volumeID + " volume metadata...")
	describeVolumeInput := &ec2.DescribeVolumesInput{
		Filters: []*ec2.Filter{
			{
				Name: aws.String("volume-id"),
				Values: []*string{
					aws.String(volumeID),
				},
			},
		},
	}
	describeVolumeResult, describeVolumeErr := service.DescribeVolumes(describeVolumeInput)
	if describeVolumeErr != nil {
		if aerr, ok := describeVolumeErr.(awserr.Error); ok {
			switch aerr.Code() {
			default:
				fmt.Println(aerr.Error())
			}
		} else {
			fmt.Println(describeVolumeErr.Error())
		}
		os.Exit(1)
	}
	return describeVolumeResult
}

func isVolumeInValidState(volumeMetadata *ec2.DescribeVolumesOutput, requiredVolumeState string) bool {
	volumeID := *volumeMetadata.Volumes[0].VolumeId
	volumeState := *volumeMetadata.Volumes[0].State
	fmt.Println("Checking if volume " + volumeID + " is in " + requiredVolumeState + " state...")
	if volumeState == requiredVolumeState {
		fmt.Println("Volume " + volumeID + " is " + requiredVolumeState + ", so we can proceed with a snapshot")
		return true
	}
	fmt.Println("Volume is " + volumeState + ", but should be " + requiredVolumeState)
	return false
}

func createVolumeSnapshot(volumeID string, volumeMetadata *ec2.DescribeVolumesOutput, service *ec2.EC2) (snapshotID string) {
	snapshotTagList := &ec2.TagSpecification{
		Tags:         volumeMetadata.Volumes[0].Tags[:],
		ResourceType: aws.String(ec2.ResourceTypeSnapshot),
	}

	fmt.Println("Creating EBS volume " + volumeID + " snapshot...")
	createSnasphotInput := &ec2.CreateSnapshotInput{
		VolumeId:          aws.String(volumeID),
		TagSpecifications: []*ec2.TagSpecification{snapshotTagList},
	}

	createSnapshotResult, createSnapshotErr := service.CreateSnapshot(createSnasphotInput)
	if createSnapshotErr != nil {
		if aerr, ok := createSnapshotErr.(awserr.Error); ok {
			switch aerr.Code() {
			default:
				fmt.Println(aerr.Error())
			}
		} else {
			fmt.Println(createSnapshotErr.Error())
		}
		os.Exit(1)
	}
	snapshotID = *createSnapshotResult.SnapshotId

	describeSnapshotInput := &ec2.DescribeSnapshotsInput{
		SnapshotIds: []*string{
			aws.String(snapshotID),
		},
	}

	fmt.Println("Waiting for snapshot " + snapshotID + " creation completion...")
	if waitForSnapshotCompletionErr := service.WaitUntilSnapshotCompleted(describeSnapshotInput); waitForSnapshotCompletionErr != nil {
		// TODO: Improve error handling :troll:
		panic(waitForSnapshotCompletionErr)
	}
	return snapshotID
}

func createEncryptedVolumeFromSnapshot(snapshotID string, kmsID string, volumeMetadata *ec2.DescribeVolumesOutput, service *ec2.EC2) (volumeID string) {
	volumeAZ := *volumeMetadata.Volumes[0].AvailabilityZone
	volumeTagList := &ec2.TagSpecification{
		Tags:         volumeMetadata.Volumes[0].Tags[:],
		ResourceType: aws.String(ec2.ResourceTypeVolume),
	}

	fmt.Println("Creating encrypted volume (KMS ID: " + kmsID + ") from snapshot " + snapshotID + " in " + volumeAZ + " and restoring the tags")
	input := &ec2.CreateVolumeInput{
		// Size and volume type should be propagated automatically from snapshot, TODO: check it
		AvailabilityZone: aws.String(volumeAZ),
		SnapshotId:       aws.String(snapshotID),
		Encrypted:        aws.Bool(true),
		// You can specify the CMK using any of the following:
		//    * Key ID. For example, key/1234abcd-12ab-34cd-56ef-1234567890ab.
		//    * Key alias. For example, alias/ExampleAlias.
		//    * Key ARN. For example, arn:aws:kms:us-east-1:012345678910:key/abcd1234-a123-456a-a12b-a123b4cd56ef.
		//    * Alias ARN. For example, arn:aws:kms:us-east-1:012345678910:alias/ExampleAlias.
		// If KMS Key speficied incorrectly, volume creation can freeze withour eror
		KmsKeyId:          aws.String(kmsID),
		TagSpecifications: []*ec2.TagSpecification{volumeTagList},
	}

	createVolumeResult, createVolumeErr := service.CreateVolume(input)
	if createVolumeErr != nil {
		if aerr, ok := createVolumeErr.(awserr.Error); ok {
			switch aerr.Code() {
			default:
				fmt.Println(aerr.Error())
			}
		} else {
			fmt.Println(createVolumeErr.Error())
		}
		os.Exit(1)
	}

	createdVolumeID := *createVolumeResult.VolumeId

	fmt.Println("Waiting for volume " + createdVolumeID + " creation completion...")
	describeNewVolumeInput := &ec2.DescribeVolumesInput{
		Filters: []*ec2.Filter{
			{
				Name: aws.String("volume-id"),
				Values: []*string{
					aws.String(createdVolumeID),
				},
			},
		},
	}
	if waitForVolumeReadinessErr := service.WaitUntilVolumeAvailable(describeNewVolumeInput); waitForVolumeReadinessErr != nil {
		panic(waitForVolumeReadinessErr)
	}

	// TODO: mekes sense to be moved to a separate function probably
	fmt.Print("Cleaning up snapshot " + snapshotID + "\n")

	deleteSnapshotInput := &ec2.DeleteSnapshotInput{
		SnapshotId: aws.String(snapshotID),
	}

	_, deleteSnapshotErr := service.DeleteSnapshot(deleteSnapshotInput)
	if deleteSnapshotErr != nil {
		if aerr, ok := deleteSnapshotErr.(awserr.Error); ok {
			switch aerr.Code() {
			default:
				fmt.Println(aerr.Error())
			}
		} else {
			fmt.Println(deleteSnapshotErr.Error())
		}
		os.Exit(1)
	}

	fmt.Println("New encrypted volume " + createdVolumeID + " is ready")
	return createdVolumeID
}

func main() {
	sourceVolumeIDPtr := flag.String("volume_id", "", "AWS EBS target volume ID.")
	kmsKeyIDPtr := flag.String("kms_key", defaultKmsKeyID, fmt.Sprintf("AWS KMS key ID (CMK) used to encrypt volume."))
	flag.Parse()
	if len(*sourceVolumeIDPtr) == 0 {
		fmt.Println("Source volume ID is not specified, getting outta here!")
		os.Exit(1)
	}

	session, service := createService()
	fmt.Println("Going to work with volume " + *sourceVolumeIDPtr + " in " + *session.Config.Region + " region")
	sourceVolumeMetadata := getVolumeMetadata(*sourceVolumeIDPtr, service)

	// This is super ugly, volumeStateIsValid should return error instead of printing and this error should be surfaced here
	if volumeStateIsValid := isVolumeInValidState(sourceVolumeMetadata, requiredVolumeState); volumeStateIsValid != true {
		os.Exit(1)
	}

	sourceVolumeSnapshotID := createVolumeSnapshot(*sourceVolumeIDPtr, sourceVolumeMetadata, service)
	createEncryptedVolumeFromSnapshot(sourceVolumeSnapshotID, *kmsKeyIDPtr, sourceVolumeMetadata, service)

}
