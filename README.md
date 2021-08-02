aws-cost [![Go Report Card](https://goreportcard.com/badge/github.com/jetbrains-infra/aws-cost)](https://goreportcard.com/report/github.com/jetbrains-infra/aws-cost) [![Pulls](https://img.shields.io/docker/pulls/jetbrainsinfra/aws-cost.svg)](https://hub.docker.com/r/jetbrainsinfra/aws-cost)
=====

This utility gets AWS billing data from [AWS Cost-Explorer](https://aws.amazon.com/aws-cost-management/aws-cost-explorer/) and return it as an influx line protocol that can be imported by [telegraf](https://github.com/influxdata/telegraf)

### TL;DR - Docker example
Docker image is located [here](https://hub.docker.com/r/jetbrainsinfra/aws-cost).
```bash
$ docker run -it --rm \
  -e AWS_ACCESS_KEY_ID=AKI... \
  -e AWS_SECRET_KEY=9eg... \
  -v $(pwd):/app/config \
  jetbrainsinfra/aws-cost
```

### Build
```bash
$ go build main.go
```

### Use
At first you have to ensure, your AWS credentials have the following permissions:
```json
{
    "Version": "2012-10-17",
    "Statement": [
        {
            "Sid": "VisualEditor0",
            "Effect": "Allow",
            "Action": [
                "ce:GetReservationUtilization",
                "ce:GetDimensionValues",
                "ce:GetCostAndUsage",
                "ce:GetTags"
            ],
            "Resource": "*"
        }
    ]
}
```

AWS Key ID and Secret ID may be taken from environment variables or from parameters of command line:
```bash
$ export AWS_ACCESS_KEY_ID=AKI...
$ export AWS_SECRET_KEY=9eg...
$ ./aws-cost
```
or:
```bash
$ ./aws-cost -key-id AKI... -secret 9eg...
```

By default will be used *yesterday* as date, but you can specify date with `-date` parameter(`YYYY-MM-DD` format):
```bash
./aws-cost -date 2019-02-10
aws-cost,account_id=25***9,service_name=EC2_-_Other cost=1.1900133074 1549756800000000000
aws-cost,account_id=25***9,service_name=Amazon_Elastic_Compute_Cloud_-_Compute cost=15.1200098849 1549756800000000000
aws-cost,account_id=25***9,service_name=AmazonCloudWatch cost=0.15 1549756800000000000
```

Telegraf input plugin settings:
```toml
[[inputs.file]]
  files = ["/tmp/aws-cost"]
  data_format = "influx"
```
And run:
```
./aws-cost -date 2019-02-10 >> /tmp/aws-cost
./aws-cost -date 2019-02-11 >> /tmp/aws-cost
./aws-cost -date 2019-02-12 >> /tmp/aws-cost
```

### Tags

You can use additional tags for your account (The idea of this feature is using different accounts in same project.), filter using Expression and also change grouping.
Warning, additional tags require second grouping to be LINKED_ACCOUNT. See [here](https://github.com/aws/aws-sdk-go-v2/blob/c698c9b1ca4c7195a49b1c19840f8528898e22e3/service/costexplorer/types/types.go) for possible values. Here is the config for example:
```json
{
  "accounts": [
    {
      "name": "main production account",
      "id": "12313...9",
      "tags": {
          "environment": "prod",
          "project": "website"
      }
    }
  ],
  "filter": {
      "And": [
          {
              "Tags": {
                  "Key": "ENV",
                  "Values": ["PROD"]
              }
          },
          {
              "Tags": {
                  "Key": "Name",
                  "Values": ["APP1","APP2"]
              }
          }
      ]
  },
  "group": [
        {
            "group_id":"REGION",
            "label":"region"
        },
        {
            "group_id":"LINKED_ACCOUNT",
            "label":"account_id"
        }
    ]
}
```
And run:
```bash
./aws-cost -config aws-cost.json -exact
aws-cost,account_id=12313...9,account_name=main,service_name=EC2_-_Other,environment=prod,project=website cost=1.1900133074 1549756800000000000
aws-cost,account_id=12313...9,account_name=main,service_name=Amazon_Elastic_Compute_Cloud_-_Compute,environment=prod,project=website cost=15.1200098849 1549756800000000000
aws-cost,account_id=12313...9,account_name=main,service_name=AmazonCloudWatch,environment=prod,project=website cost=0.15 1549756800000000000
```
Also, it's not required to use flag `-exact` with `-config` but with `-exact` you will get only accouts that exists in `aws-cost.json`.
