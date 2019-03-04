aws-cost [![Go Report Card](https://goreportcard.com/badge/github.com/jetbrains-infra/aws-cost)](https://goreportcard.com/report/github.com/jetbrains-infra/aws-cost) 
[![Pulls](https://img.shields.io/docker/pulls/vebeer/urlsh.svg)](https://img.shields.io/docker/pulls/vebeer/urlsh.svg)
=====

This utility gets AWS billing data from [AWS Cost-Explorer](https://aws.amazon.com/aws-cost-management/aws-cost-explorer/) and return it as an influx line protocol that can be imported by [telegraf](https://github.com/influxdata/telegraf)

### Build
```
$ go biuld main.go
```

### Examples
By default will be used *yesterday* as date, but you can specify these values as you wish.

```
$ ./aws-cost -key-id AKI... -secret 9eg...
aws-cost,account_id=25***9,service_name=EC2_-_Other cost=0.7933431449 1550361600000000000
aws-cost,account_id=25***9,service_name=Amazon_Elastic_Compute_Cloud_-_Compute cost=11.3400006485 1550361600000000000
aws-cost,account_id=25***9,service_name=AmazonCloudWatch cost=0.1 1550361600000000000
```

Or key ID and secret ID may be taken from environment variables:
```
$ export AWS_ACCESS_KEY_ID=AKI...
$ export AWS_SECRET_KEY=9eg...
$ ./aws-cost
```

Also you can specify the report date:
```
./aws-cost -key-id AKI... -secret 9eg... -date 2019-02-10
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

Moreover, you can use additional tags for your account. The idea of this feature is using different accounts in same project. Here is the config for example:
```json
{
  "projects": [
    {
      "project_name": "project1",
      "accounts": [
        {
          "name": "account1",
          "id": "79...56"
        },
        {
          "name": "account2",
          "id": "83...80"
        }
      ]
    },
    {
      "project_name": "project2",
      "accounts": [
        {
          "name": "account1",
          "id": "91...78"
        }
      ]
    }
  ]
}
```
And run:
```
./aws-cost -config aws-cost.json -exact
aws-cost,account_id=25***9,service_name=EC2_-_Other,project=project1 cost=1.1900133074 1549756800000000000
aws-cost,account_id=25***9,service_name=Amazon_Elastic_Compute_Cloud_-_Compute,project=project1 cost=15.1200098849 1549756800000000000
aws-cost,account_id=25***9,service_name=AmazonCloudWatch,project=project1 cost=0.15 1549756800000000000
```
It's not required to use flag `-exact` with `-config`.

### Docker example
Docker image is located [here](https://cloud.docker.com/u/jetbrainsinfra/repository/docker/jetbrainsinfra/aws-cost)

```
$ docker run -it --rm \
  -e AWS_ACCESS_KEY_ID=AKI... \
  -e AWS_SECRET_KEY=9eg... \
  -v $(pwd):/app/config \
  jetbrainsinfra/aws-cost:latest \
  -date 2019-02-26 \
  -config /app/config/aws-cost-test.json -exact
```
