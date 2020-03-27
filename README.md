# Native Secrets 

This repo is a proof-of-concept (PoC) showing how to inject AWS secret manager secrets to pods. The basic idea of the PoC is to use an extension point of the Kubernetes API server called [dynamic admission control](https://kubernetes.io/docs/reference/access-authn-authz/extensible-admission-controllers/): when a user creates a pod with annotations, a mutating Webhook (implemented as an AWS Lambda function) intercepts the process and adds a init container to the pod which will read the secrets from secrets manager and injects the secrets to main container through a shared volume.

## Installation

In order to build and deploy the service, clone this repo and make sure you've got the following available, locally:

- The [aws](https://docs.aws.amazon.com/cli/latest/userguide/cli-chap-install.html) CLI
- The [SAM CLI](https://github.com/awslabs/aws-sam-cli)
- Go 1.12 or above
- A Kubernetes 1.14 cluster or above with `kubectl` configured, locally

Additionally, I recommend that you have [jq](https://stedolan.github.io/jq/download/) installed.

First, prepare the S3 bucket for the Lambda function that provides the webhook (make sure that you pick different name for the `WEBHOOK_BUCKET` bucket env variable since buckets need to be globally unique):

```sh
export WEBHOOK_BUCKET=nase-webhook

aws s3api create-bucket \
          --bucket $WEBHOOK_BUCKET \
          --create-bucket-configuration LocationConstraint=$(aws configure get region) \
          --region $(aws configure get region)
```

Now, to install the webhook, execute:

```sh
make deploy
``` 

## Usage

```sh
$ kubectl create -f pod2.yml

```
