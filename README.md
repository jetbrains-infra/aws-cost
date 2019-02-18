aws-cost
=====

This utility gets AWS billing data from [AWS Cost-Explorer](https://aws.amazon.com/aws-cost-management/aws-cost-explorer/) and return it as an influx line protocol that can be imported by [telegraf](https://github.com/influxdata/telegraf)

### Build
```
$ go biuld main.go
```

### Examples
By default will be used "eu-central-1" as AWS region and *yesterday* as date, but you can specify these values as you wish.

```
$ ./aws-cost -region eu-central-1 -key-id AKI... -secret 9eg...
aws-cost,account_id=25***9,region=eu-central-1,service_name=EC2_-_Other cost=0.7933431449 1550361600000000000
aws-cost,account_id=25***9,region=eu-central-1,service_name=Amazon_Elastic_Compute_Cloud_-_Compute cost=11.3400006485 1550361600000000000
aws-cost,account_id=25***9,region=eu-central-1,service_name=AmazonCloudWatch cost=0.1 1550361600000000000
```

Or key ID and secret ID may be taken from environment variables:
```
$ export AWS_ACCESS_KEY_ID=AKI...
$ export AWS_SECRET_KEY=9eg...
$ ./aws-cost
```

Also you can specify the report date:
```
./aws-cost -region eu-central-1 -key-id AKI... -secret 9eg... -date 2019-02-10
aws-cost,account_id=25***9,region=eu-central-1,service_name=EC2_-_Other cost=1.1900133074 1549756800000000000
aws-cost,account_id=25***9,region=eu-central-1,service_name=Amazon_Elastic_Compute_Cloud_-_Compute cost=15.1200098849 1549756800000000000
aws-cost,account_id=25***9,region=eu-central-1,service_name=AmazonCloudWatch cost=0.15 1549756800000000000
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
