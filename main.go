package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"io/ioutil"
	"log"
	"os"
	"context"
	"strconv"
	"strings"
	"time"
	"github.com/aws/aws-sdk-go-v2/config"
	"github.com/aws/aws-sdk-go-v2/service/costexplorer"
	"github.com/aws/aws-sdk-go-v2/service/costexplorer/types"
)

var (
	date       				string
	keyID      				string
	secretKey  				string
	configFile 				string
	resultPath 				string
	exact      				bool
	logLevel   				string
	region					string
	tagsList   				[]string
	groupDefinitionLabels	[]groupDefinitionLabel
	defaultTag = 			"Unknown"

	resultFile  			*os.File
	debugLogger 			*log.Logger
	traceLogger 			*log.Logger
)

type settings struct {
	AWSKeyID     			string `json:"aws_key_id,omitempty"`
	AWSSecretKey 			string `json:"aws_secret_key,omitempty"`
	Date         			string `json:"date"`
}

type Config struct {
	Accounts []struct {
		Name string            `json:"name"`
		ID   string            `json:"id"`
		Tags map[string]string `json:"tags,omitempty"`
	} `json:"accounts"`
}

type serviceCost struct {
	SecondGroupKey   string `json:"second_group_key"`
	FirstGroupKey string `json:"first_group_key"`
	ServiceCost string `json:"service_cost"`
	Timestamp   string `json:"timestamp"`
}

type groupDefinitionLabel struct {
	groupId   	string
	label		string
}

func init() {
	var err error

	flag.StringVar(&date, "date", "", "date in format yyyy-MM-dd, (by default will be set as yesterday)")
	flag.StringVar(&keyID, "key-id", "", "AWS key ID(by default will be taken from env AWS_ACCESS_KEY_ID)")
	flag.StringVar(&secretKey, "secret", "", "AWS secret key(by default will be taken from env AWS_SECRET_KEY)")
	flag.StringVar(&region, "region", "eu-west-1", "region")
	flag.StringVar(&configFile, "config", "", "config file")
	flag.StringVar(&resultPath, "result", "", "result file")
	flag.StringVar(&logLevel, "log", "", "log level(only 'debug' is supported right now)")
	flag.BoolVar(&exact, "exact", false, "show only accounts from config file")

	flag.Parse()

	if exact && configFile == "" {
		flag.PrintDefaults()
		os.Exit(1)
	}

	if keyID != "" {
		os.Setenv("AWS_ACCESS_KEY_ID",keyID)
	}

	if secretKey != "" {
		os.Setenv("AWS_SECRET_ACCESS_KEY",secretKey)
	}

	if region != "" {
		os.Setenv("AWS_REGION",region)
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

	groupDefinitionLabels = make([]groupDefinitionLabel,2)
	groupDefinitionLabels[0] = groupDefinitionLabel{
		groupId: "SERVICE",
		label: "service_name",
	}
	groupDefinitionLabels[1] = groupDefinitionLabel{
		groupId: "LINKED_ACCOUNT",
		label: "account_name",
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

	for _, acc := range c.Accounts {
		if len(acc.Tags) > 0 {
			for t := range acc.Tags {
				_ = addTags(t)
			}
		}
	}

	debugLogger.Printf("config loaded\n")
	return c, err
}

func addTags(tag string) bool {
	for _, t := range tagsList {
		if t == tag {
			return false
		}
	}
	tagsList = append(tagsList, tag)

	return true
}


func getDataFromAWS(a *settings) (*[]types.ResultByTime, error) {
	var err error
	var ctx = context.TODO()

	debugLogger.Printf("collecting data from AWS\n")

	t, _ := time.Parse("2006-01-02", date)
	end := t.AddDate(0, 0, 1).Format("2006-01-02")

	cfg, err := config.LoadDefaultConfig(ctx)

	ce := costexplorer.NewFromConfig(cfg)

	data, err := ce.GetCostAndUsage(ctx, &costexplorer.GetCostAndUsageInput{
		Granularity: "DAILY",
		Metrics:     []string{"UnblendedCost"},
		GroupBy: []types.GroupDefinition{{
			Type: "DIMENSION",
			Key:  &groupDefinitionLabels[0].groupId,
		}, {
			Type: "DIMENSION",
			Key:  &groupDefinitionLabels[1].groupId,
		}},
		TimePeriod: &types.DateInterval{
			Start: &date,
			End:   &end,
		},
	})

	if err != nil {
		return nil, fmt.Errorf("failed to do request, %v", err)
	}

	debugLogger.Printf("collected %v items from AWS\n", len(data.ResultsByTime[0].Groups))
	return &data.ResultsByTime, err
}

func getServiceCost(results *[]types.ResultByTime) []serviceCost {
	sc := []serviceCost{}
	t, _ := time.Parse("2006-01-02", date)
	timestamp := strconv.FormatInt(t.UnixNano(), 10)

	for _, timePeriod := range *results {
		for _, group := range timePeriod.Groups {
			sc = append(sc, serviceCost{
				SecondGroupKey:   group.Keys[1],
				FirstGroupKey: strings.Replace(group.Keys[0], " ", "_", -1),
				ServiceCost: *group.Metrics["UnblendedCost"].Amount,
				Timestamp:   timestamp,
			})
		}
	}
	return sc
}

func printInfluxLineProtocol(servicesFromAWS []serviceCost, c Config) {
	debugLogger.Printf("printing result in Influx Line Protocol to %s\n", resultFile.Name())
	if len(c.Accounts) == 0 {
		for _, s := range servicesFromAWS {
			fmt.Fprintf(resultFile,
				"aws-cost,%v=%v,%v=%v cost=%v %v\n",
				groupDefinitionLabels[1].label,
				s.SecondGroupKey,
				groupDefinitionLabels[0].label,
				s.FirstGroupKey,
				s.ServiceCost,
				s.Timestamp)
		}
	} else {
		for _, s := range servicesFromAWS {
			if exact {
				if ok, accountName, accountTags := checkElementInArray(c, s.SecondGroupKey); ok {
					fmt.Fprintf(resultFile,
						"aws-cost,%v=%v,account_name=%v,%v=%v%v cost=%v %v\n",
						groupDefinitionLabels[1].label,
						s.SecondGroupKey,
						accountName,
						groupDefinitionLabels[0].label,
						s.FirstGroupKey,
						accountTags,
						s.ServiceCost,
						s.Timestamp)
				}
			} else {
				if ok, accountName, accountTags := checkElementInArray(c, s.SecondGroupKey); ok {
					fmt.Fprintf(resultFile,
						"aws-cost,%v=%v,account_name=%v,%v=%v%v cost=%v %v\n",
						groupDefinitionLabels[1].label,
						s.SecondGroupKey,
						accountName,
						groupDefinitionLabels[0].label,
						s.FirstGroupKey,
						accountTags,
						s.ServiceCost,
						s.Timestamp)
				} else {
					fmt.Fprintf(resultFile,
						"aws-cost,%v=%v,account_name=%v,%v=%v cost=%v %v\n",
						groupDefinitionLabels[1].label,
						s.SecondGroupKey,
						accountName,
						s.FirstGroupKey,
						s.ServiceCost,
						s.Timestamp)
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
			currentTags := acc.Tags
			if currentTags == nil {
				currentTags = make(map[string]string)
			}
			if len(currentTags) != len(tagsList) {
				for _, t := range tagsList {
					if _, elementExist := currentTags[t]; !elementExist {
						//debugLogger.Println(currentTags, t, defaultTag)
						currentTags[t] = defaultTag
					}
				}
			}

			name := strings.Replace(acc.Name, " ", "_", -1)
			tags := getStringWithTags(currentTags)
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
