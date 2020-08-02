### Preamble

As you might have guessed or noticed, AWS does not support on-the-fly encryption of EBS volumes (as of 01.08.2020), so this can only be performed through snapshotting and restoring with modified volume spec.

***Key features of this stupidly simple tool:***

- makes a snapshot of EBS volume
- creates a new encrypted EBS volume with the same tags in the same AZ with the same spec (volume type and size are not passed explicitly since it happens under the hood through volume metadata stored in the snapshot)
- eventually performs a cleanup of the snapshot

- does not remove or untag the old volume
- please do not expect any other automagic from it

### Usage

It will use default AWS SDK credentials chain, so make sure to pass AWS credentials in one way or another. The KMS key ID is optional, hardcoded default is `alias/aws/ebs` (the same one that is implicitly used if you create encrypted snapshot without passing KMS key at all).

Usage example:

```bash
$ AWS_REGION=us-west-2 go run main.go --volume_id=vol-00000000000001337
vol-0d93008cbb8175778

Creating AWS client session...
Creating EC2 service...
Going to work with volume vol-00000000000001337 in us-west-2 region
Getting vol-00000000000001337 volume metadata...
Checking if volume vol-00000000000001337 is in available state...
Volume vol-00000000000001337 is available, so we can proceed with a snapshot
Creating EBS volume vol-00000000000001337 snapshot...
Waiting for snapshot snap-00000000000001337 creation completion...
Creating encrypted volume (KMS ID: alias/aws/ebs) from snapshot snap-00000000000001337 in us-west-2a and restoring the tags
Waiting for volume vol-00000000133701337 creation completion...
Cleaning up snapshot snap-00000000000001337
New encrypted volume vol-00000000133701337 is ready
```

If you'd like to get more bells and whistles, your Pull Requests are always welcomed!

### Current limitations

- Code quality is "i scraped it in few hours while drinking beer"
- Error handling is a joke
- Logging and overal verbocity is just enough if you want to perform "cut a trees with an axe" type of activity
- AWS API throttling might screw you over, no retries are provided
