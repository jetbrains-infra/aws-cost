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
	resultPath string
	exact      bool
	logLevel   string

	resultFile  *os.File
	debugLogger *log.Logger
	traceLogger *log.Logger
)

type settings struct {
	AWSKeyID     string `json:"aws_key_id"`
	AWSSecretKey string `json:"aws_secret_key"`
	Date         string `json:"date"`
}

type Config struct {
	Accounts []struct {
		Name string            `json:"name"`
		ID   string            `json:"id"`
		Tags map[string]string `json:"tags,omitempty"`
	} `json:"accounts"`
}

type serviceCost struct {
	AccountID   string `json:"account_id"`
	ServiceName string `json:"service_name"`
	ServiceCost string `json:"service_cost"`
	Timestamp   string `json:"timestamp"`
}

func init() {
	var err error

	flag.StringVar(&date, "date", "", "date in format yyyy-MM-dd, (by default will be set as yesterday)")
	flag.StringVar(&keyID, "key-id", "", "AWS key ID(by default will be taken from env AWS_ACCESS_KEY_ID)")
	flag.StringVar(&secretKey, "secret", "", "AWS secret key(by default will be taken from env AWS_SECRET_KEY)")
	flag.StringVar(&configFile, "config", "", "config file")
	flag.StringVar(&resultPath, "result", "", "result file")
	flag.StringVar(&logLevel, "log", "", "log level(only 'debug' is supported right now)")
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

	resultFile = os.Stdout
	if resultPath != "" {
		_ = os.Remove(resultPath)
		resultFile, err = os.OpenFile(resultPath, os.O_CREATE|os.O_WRONLY, 0666)
		if err != nil {
			log.Fatalf("failed opening file: %s", err)
		}
	}

	debugLogger = log.New(ioutil.Discard, "", -1)
	traceLogger = log.New(ioutil.Discard, "", -1)
	if logLevel == "debug" {
		debugLogger = log.New(os.Stdout, "DEBUG:", log.Ldate|log.Ltime|log.Lshortfile)
	} else if logLevel == "trace" {
		debugLogger = log.New(os.Stdout, "DEBUG:", log.Ldate|log.Ltime|log.Lshortfile)
		traceLogger = log.New(os.Stdout, "TRACE:", log.Ldate|log.Lmicroseconds|log.Lshortfile)
	}
}

func loadConfig(path string) (Config, error) {
	var c Config
	var err error

	debugLogger.Printf("load config: %v\n", path)

	file, err := ioutil.ReadFile(path)
	if err != nil {
		return c, fmt.Errorf("can't load config: %v", err)
	}

	err = json.Unmarshal(file, &c)
	if err != nil {
		return c, fmt.Errorf("can't unmarshal config: %v", err)
	}

	debugLogger.Printf("config loaded\n")
	return c, err
}

func getDataFromAWS(a *settings) (*[]costexplorer.ResultByTime, error) {
	var err error
	groupDefinitions := []string{"SERVICE", "LINKED_ACCOUNT"}

	debugLogger.Printf("collecting data from AWS\n")

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

	debugLogger.Printf("collected %v items from AWS\n", len(data.ResultsByTime[0].Groups))
	return &data.ResultsByTime, err
}

func getServiceCost(results *[]costexplorer.ResultByTime) []serviceCost {
	sc := []serviceCost{}
	t, _ := time.Parse("2006-01-02", date)
	timestamp := strconv.FormatInt(t.UnixNano(), 10)

	for _, timePeriod := range *results {
		for _, group := range timePeriod.Groups {
			sc = append(sc, serviceCost{
				AccountID:   group.Keys[1],
				ServiceName: strings.Replace(group.Keys[0], " ", "_", -1),
				ServiceCost: *group.Metrics["UnblendedCost"].Amount,
				Timestamp:   timestamp,
			})
		}
	}
	return sc
}

func printInfluxLineProtocol(servicesFromAWS []serviceCost, c Config) {
	debugLogger.Printf("printing result in Influx Line Protocol\n")
	if len(c.Accounts) == 0 {
		for _, s := range servicesFromAWS {
			fmt.Fprintf(resultFile, "aws-cost,account_id=%v,service_name=%v cost=%v %v\n", s.AccountID, s.ServiceName, s.ServiceCost, s.Timestamp)
		}
	} else {
		for _, s := range servicesFromAWS {
			if exact {
				if ok, accountName, accountTags := checkElementInArray(c, s.AccountID); ok {
					fmt.Fprintf(resultFile, "aws-cost,account_id=%v,account_name=%v,service_name=%v%v cost=%v %v\n", s.AccountID, accountName, s.ServiceName, accountTags, s.ServiceCost, s.Timestamp)
				}
			} else {
				if ok, accountName, accountTags := checkElementInArray(c, s.AccountID); ok {
					fmt.Fprintf(resultFile, "aws-cost,account_id=%v,account_name=%v,service_name=%v%v cost=%v %v\n", s.AccountID, accountName, s.ServiceName, accountTags, s.ServiceCost, s.Timestamp)
				} else {
					fmt.Fprintf(resultFile, "aws-cost,account_id=%v,account_name=%v,service_name=%v cost=%v %v\n", s.AccountID, accountName, s.ServiceName, s.ServiceCost, s.Timestamp)
				}
			}
		}
	}
}

func checkElementInArray(config Config, element string) (bool, string, string) {
	if element[0] == '0' {
		element = element[1:]
	}
	for _, acc := range config.Accounts {
		if acc.ID == element {
			name := strings.Replace(acc.Name, " ", "_", -1)
			tags := getStringWithTags(acc.Tags)
			return true, name, tags
		}
	}
	return false, "", ""
}

func getStringWithTags(accountTags map[string]string) string {
	tags := ""
	for tag := range accountTags {
		tags += fmt.Sprintf(",%s=%s", tag, accountTags[tag])
	}

	ret := strings.Replace(tags, " ", "_", -1)

	return ret
}

func main() {
	var c Config
	var err error
	defer resultFile.Close()

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
