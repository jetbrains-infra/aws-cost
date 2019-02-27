package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"io/ioutil"
	"log"
	"os"
	"strconv"
	"strings"
	"time"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/aws/external"
	"github.com/aws/aws-sdk-go-v2/service/costexplorer"
)

var (
	date       string
	keyID      string
	secretKey  string
	configFile string
	exact      bool
)

type settings struct {
	AWSKeyID     string `json:"aws_key_id"`
	AWSSecretKey string `json:"aws_secret_key"`
	Date         string `json:"date"`
}

type config struct {
	Categories []struct {
		Name     string   `json:"name"`
		Accounts []string `json:"accounts"`
	} `json:"categories"`
}

type serviceCost struct {
	AccountID   string `json:"account_id"`
	ServiceName string `json:"service_name"`
	ServiceCost string `json:"service_cost"`
	Timestamp   string `json:"timestamp"`
}

func init() {
	flag.StringVar(&date, "date", "", "date in format yyyy-MM-dd, (by default will be set as yesterday)")
	flag.StringVar(&keyID, "key-id", "", "AWS key ID(by default will be taken from env AWS_ACCESS_KEY_ID)")
	flag.StringVar(&secretKey, "secret", "", "AWS secret key(by default will be taken from env AWS_SECRET_KEY)")
	flag.StringVar(&configFile, "config", "", "config file")
	flag.BoolVar(&exact, "exact", false, "show only accounts from config file")

	flag.Parse()

	if exact && configFile == "" {
		flag.PrintDefaults()
		os.Exit(1)
	}

	if keyID == "" {
		keyID = os.Getenv("AWS_ACCESS_KEY_ID")
	}

	if secretKey == "" {
		secretKey = os.Getenv("AWS_SECRET_KEY")
	}

	if keyID == "" || secretKey == "" {
		flag.PrintDefaults()
		os.Exit(1)
	}

	if date == "" {
		yesterday := time.Now().AddDate(0, 0, -1)
		date = yesterday.Format("2006-01-02")
	}
}

func loadConfig(path string) (config, error) {
	var (
		c   config
		err error
	)
	file, err := ioutil.ReadFile(path)
	if err != nil {
		return c, fmt.Errorf("can't load config: %v", err)
	}

	err = json.Unmarshal(file, &c)
	if err != nil {
		return c, fmt.Errorf("can't unmarshal config: %v", err)
	}

	return c, err
}

func getDataFromAWS(a *settings) (*[]costexplorer.ResultByTime, error) {
	var err error
	groupDefinitions := []string{"SERVICE", "LINKED_ACCOUNT"}

	t, _ := time.Parse("2006-01-02", date)
	end := t.AddDate(0, 0, 1).Format("2006-01-02")

	cfg, err := external.LoadDefaultAWSConfig(
		external.WithCredentialsValue(aws.Credentials{
			AccessKeyID:     a.AWSKeyID,
			SecretAccessKey: a.AWSSecretKey,
		}),
	)
	if err != nil {
		return nil, fmt.Errorf("failed to load config, %v", err)
	}

	ce := costexplorer.New(cfg)

	request := ce.GetCostAndUsageRequest(&costexplorer.GetCostAndUsageInput{
		Granularity: "DAILY",
		Metrics:     []string{"UnblendedCost"},
		GroupBy: []costexplorer.GroupDefinition{{
			Type: "DIMENSION",
			Key:  &groupDefinitions[0],
		}, {
			Type: "DIMENSION",
			Key:  &groupDefinitions[1],
		}},
		TimePeriod: &costexplorer.DateInterval{
			Start: &date,
			End:   &end,
		},
	})

	data, err := request.Send()
	if err != nil {
		return nil, fmt.Errorf("failed to do request, %v", err)
	}

	return &data.ResultsByTime, err
}

func getServiceCost(results *[]costexplorer.ResultByTime) []serviceCost {
	sc := []serviceCost{}
	t, _ := time.Parse("2006-01-02", date)
	timestamp := strconv.FormatInt(t.UnixNano(), 10)

	for _, timePeriod := range *results {
		for _, group := range timePeriod.Groups {
			sc = append(sc, serviceCost{
				AccountID: group.Keys[1],
				// Region:      region,
				ServiceName: strings.Replace(group.Keys[0], " ", "_", -1),
				ServiceCost: *group.Metrics["UnblendedCost"].Amount,
				Timestamp:   timestamp,
			})
		}
	}

	return sc
}

func printInfluxLineProtocol(sc []serviceCost, c config) {
	if len(c.Categories) > 0 {
		for _, s := range sc {
			for _, category := range c.Categories {
				if exact {
					if checkElementInArray(category.Accounts, s.AccountID) {
						fmt.Printf("aws-cost,account_id=%v,service_name=%v,category=%v cost=%v %v\n", s.AccountID, s.ServiceName, category.Name, s.ServiceCost, s.Timestamp)
					}
				} else {
					if checkElementInArray(category.Accounts, s.AccountID) {
						fmt.Printf("aws-cost,account_id=%v,service_name=%v,category=%v cost=%v %v\n", s.AccountID, s.ServiceName, category.Name, s.ServiceCost, s.Timestamp)
					} else {
						fmt.Printf("aws-cost,account_id=%v,service_name=%v cost=%v %v\n", s.AccountID, s.ServiceName, s.ServiceCost, s.Timestamp)
					}
				}
			}
		}
	} else {
		for _, s := range sc {
			fmt.Printf("aws-cost,account_id=%v,service_name=%v cost=%v %v\n", s.AccountID, s.ServiceName, s.ServiceCost, s.Timestamp)
		}
	}
}

func checkElementInArray(array []string, e string) bool {
	for _, ae := range array {
		if ae == e {
			return true
		}
	}
	return false
}

func main() {
	var (
		c   config
		err error
	)

	if configFile != "" {
		c, err = loadConfig(configFile)
		if err != nil {
			log.Fatal(err)
		}
	}

	data, err := getDataFromAWS(&settings{
		AWSKeyID:     keyID,
		AWSSecretKey: secretKey,
		Date:         date,
	})
	if err != nil {
		log.Fatal(err)
	}

	printInfluxLineProtocol(getServiceCost(data), c)
}
