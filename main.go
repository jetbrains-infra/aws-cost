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
	logLevel   string

	debugLogger *log.Logger
	traceLogger *log.Logger
)

type settings struct {
	AWSKeyID     string `json:"aws_key_id"`
	AWSSecretKey string `json:"aws_secret_key"`
	Date         string `json:"date"`
}

type config struct {
	Projects []struct {
		Name     string          `json:"project_name"`
		Accounts []accountConfig `json:"accounts"`
	} `json:"projects"`
}

type accountConfig struct {
	Name string `json:"name"`
	ID   string `json:"id"`
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
	flag.StringVar(&logLevel, "log", "", "log level(only 'debug' is supported right now)")

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

	debugLogger = log.New(ioutil.Discard, "", -1)
	traceLogger = log.New(ioutil.Discard, "", -1)
	if logLevel == "debug" {
		debugLogger = log.New(os.Stdout, "DEBUG:", log.Ldate|log.Ltime|log.Lshortfile)
	} else if logLevel == "trace" {
		debugLogger = log.New(os.Stdout, "DEBUG:", log.Ldate|log.Ltime|log.Lshortfile)
		traceLogger = log.New(os.Stdout, "TRACE:", log.Ldate|log.Lmicroseconds|log.Lshortfile)
	}
}

func loadConfig(path string) (config, error) {
	var c config
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

func printInfluxLineProtocol(servicesFromAWS []serviceCost, c config) {
	debugLogger.Printf("printing result in Influx Line Protocol\n")
	if len(c.Projects) == 0 {
		for _, s := range servicesFromAWS {
			fmt.Printf("aws-cost,account_id=%v,service_name=%v cost=%v %v\n", s.AccountID, s.ServiceName, s.ServiceCost, s.Timestamp)
		}
	} else {
		for _, s := range servicesFromAWS {
			//traceLogger.Printf("printInfluxLineProtocol(aws account id = %v):\n", s.AccountID)
			if exact {
				if ok, projectName, accountName := checkElementInArray(c, s.AccountID); ok {
					// traceLogger.Printf("printInfluxLineProtocol(aws account id = %v, project from cfg = %v):\n", s.AccountID, c)
					// traceLogger.Printf("\texact, ok\n")
					fmt.Printf("aws-cost,account_id=%v,service_name=%v,project=%v,account_name=%v cost=%v %v\n", s.AccountID, s.ServiceName, projectName, accountName, s.ServiceCost, s.Timestamp)
				}
			} else {
				if ok, projectName, accountName := checkElementInArray(c, s.AccountID); ok {
					// traceLogger.Printf("printInfluxLineProtocol(aws account id = %v, project from cfg = %v):\n", s.AccountID, c)
					// traceLogger.Printf("\tnot exact, ok\n")
					fmt.Printf("aws-cost,account_id=%v,service_name=%v,project=%v,account_name=%v cost=%v %v\n", s.AccountID, s.ServiceName, projectName, accountName, s.ServiceCost, s.Timestamp)
				} else {
					// traceLogger.Printf("printInfluxLineProtocol(aws account id = %v, project from cfg = %v):\n", s.AccountID, projectName)
					// traceLogger.Printf("\tnot exact, not ok\n")
					fmt.Printf("aws-cost,account_id=%v,service_name=%v cost=%v %v\n", s.AccountID, s.ServiceName, s.ServiceCost, s.Timestamp)
				}
			}
		}
	}
}

func checkElementInArray(projects config, element string) (bool, string, string) {
	//traceLogger.Printf("checkElementInArray, element = %v:\n", element)
	for _, proj := range projects.Projects {
		for _, arrayElement := range proj.Accounts {
			//traceLogger.Printf("\tarrayElement = %v\n", arrayElement)
			if arrayElement.ID == element {
				//traceLogger.Printf("\t\tMATCH\n")
				return true, proj.Name, arrayElement.Name
			}
		}
	}
	return false, "", ""
}

func main() {
	var c config
	var err error

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
