# Preamble

So if you stumbled across this repo to read this README file with one single purpose - to overcome kops  [limitation](https://github.com/kubernetes/kops/blob/master/docs/operations/etcd_backup_restore_encryption.md#etcd-volume-encryption) to not being able to encrypt etcd cluster volumes on running cluster, even while having shiny new `etcd-manager` managing your 3+ node etcd cluster, then you are in the right place. This was tested multiple times on ***AWS*** and will probably work in GCP with minor adjustments. Your security department will be proud of you!

There are multiple ways to hack your way through, so the guide below is just my opinionated best (read as "easiest/fastest/reliable") sequence of actions to achieve this.

## Encrypt etcd volumes on running Kubernetes cluster managed by kops in AWS, wonky how-to

### Preparation

Make sure you have multi-node etcd cluster, otherwise you would need to perform another painful maintenance to convert it to multi-node. Consult this [guide](https://github.com/kubernetes/kops/blob/master/docs/single-to-multi-master.md) for more details on how to make it happen. As an alternative you can still perform this on a single node cluster by bringing it down for maintenance (***NOT RECOMMENDED BY ANY MEANS***).

### Working around kops validation

Now we need to bypass kops cluster spec validation, which won't allow us to simply change `encryptedVolume` fields for each etcd member to `true`. There are two ways of doing this: we can simply download current kops cluster spec from S3 bucket, modify these fields and re-upload modified spec back.

A bit less hacky option would be to use kops binary itself to make this happen (just to have all other bundled state validations serving us to reduce the chances to destroy our cluster completely). Depending on how you manage your cluster state in a declarative way you need to execute `kops edit cluster` or `kops replace -f` in two steps, by first changing the names of all etcd members:

This part of cluster spec

```yaml
spec:
  ...
  etcdClusters:
    - name: main
      etcdMembers:
        - instanceGroup: master-us-west-2a
          name: a
        - instanceGroup: master-us-west-2b
          name: b
        - instanceGroup: master-us-west-2c
          name: c
```

Will be changed to:

```yaml
spec:
  ...
  etcdClusters:
    - name: main
      etcdMembers:
        - instanceGroup: master-us-west-2a
          name: a1
        - instanceGroup: master-us-west-2b
          name: b1
        - instanceGroup: master-us-west-2c
          name: c1
```

And only after this we can fool kops validations and enable (not really, we will enable it later) etcd volumes encryption (don't forget to revert changes to etcd cluster member names):

```yaml
spec:
  ...
  etcdClusters:
    - name: main
      etcdMembers:
        - instanceGroup: master-us-west-2a
          name: a
          encryptedVolume: true
        - instanceGroup: master-us-west-2b
          name: b
          encryptedVolume: true
        - instanceGroup: master-us-west-2c
          name: c
          encryptedVolume: true
```

### Getting the actual shit done

So now we have a discreptancy between cluster spec stored in S3 bucket and the actual state of our volumes. Knowing that kops won't store etcd EBS volume IDs and the actual volume discovery during master node startup relies **ONLY** on EBS volume tags, we are going to encrypt and replace these volumes one by one.

Assuming that you have vanilla 3+ (don't ask me why would someone have more than 3 master nodes) master setup configured by kops which should look like 3+ AWS autoscaling groups, ideally one per one each availability zone, we are going to perform **etcd volume encryption rolling upgrade** for each ASG.

For this we need to modify first ASG with Kubernetes master by reducing desired/minimum/maximum capacities from ***1*** to **0** and after it's done, to terminate the EC2 instance with Kubernetes master in the same AZ and wait for first etcd member volumes to change their state from `in-use` to `available` (by default you should have ***two** etcd clusters per Kubernetes cluster, the main one and the second one to store only Kubernetes events).

Now we need to create snapshots of these etcd volumes and then to create new encrypted volumes from snapshots by copying volume tags from current non-encrypted etcd volumes to new ones. I hacked together a stupidly simple tool to automate this process, which you can find in `ebs-encrypt` folder. After it's done, we need to remove all the tags from old etcd volume (again, etcd-manager has volume discovery that is based on volume tags). We are now ready to change ASG capacity back to ***1/1/1*** and to wait until our previously terminated master starts and becomes ready. After master is back online, we can perform the same sequence of actions for the rest of Kubernetes masters.

### Making sure it works

When all the etcd members are successfully migrated to encrypted volumes, you can verify that kops is happy with them by executing `kops cluster update` and seeing zero planned changes to the cluster state.
