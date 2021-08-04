package main

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"github.com/aws/aws-sdk-go-v2/config"
	"github.com/aws/aws-sdk-go-v2/service/costexplorer"
	"github.com/aws/aws-sdk-go-v2/service/costexplorer/types"
	"github.com/influxdata/influxdb-client-go/v2"
	"github.com/influxdata/influxdb-client-go/v2/api"
	"io/ioutil"
	"log"
	"os"
	"reflect"
	"strconv"
	"strings"
	"time"
)

var (
	date                  string
	keyID                 string
	secretKey             string
	configFile            string
	resultPath            string
	exact                 bool
	logLevel              string
	region                string
	tagsList              []string
	globalTags            map[string]string
	groupDefinitionLabels []groupDefinitionLabel
	defaultTag            = "Unknown"
	client                influxdb2.Client
	writeAPI              api.WriteAPIBlocking
	influxUrl             string

	resultFile  *os.File
	debugLogger *log.Logger
	traceLogger *log.Logger
	filter      types.Expression
)

type settings struct {
	AWSKeyID     string `json:"aws_key_id,omitempty"`
	AWSSecretKey string `json:"aws_secret_key,omitempty"`
	Date         string `json:"date"`
}

type Config struct {
	Accounts []struct {
		Name string            `json:"name"`
		ID   string            `json:"id"`
		Tags map[string]string `json:"tags,omitempty"`
	} `json:"accounts"`
	Filter     types.Expression       `json:"filter,omitempty"`
	Group      []groupDefinitionLabel `json:"group,omitempty"`
	GlobalTags map[string]string      `json:"tags,omitempty"`
}

type serviceCost struct {
	SecondGroupKey string `json:"second_group_key"`
	FirstGroupKey  string `json:"first_group_key"`
	ServiceCost    string `json:"service_cost"`
	Timestamp      string `json:"timestamp"`
}

type groupDefinitionLabel struct {
	GroupId string `json:"group_id"`
	Label   string `json:"label"`
}

func init() {
	var err error
	var influxToken string

	flag.StringVar(&date, "date", "", "date in format yyyy-MM-dd, (by default will be set as yesterday)")
	flag.StringVar(&keyID, "key-id", "", "AWS key ID(by default will be taken from env AWS_ACCESS_KEY_ID)")
	flag.StringVar(&secretKey, "secret", "", "AWS secret key(by default will be taken from env AWS_SECRET_KEY)")
	flag.StringVar(&region, "region", "eu-west-1", "region")
	flag.StringVar(&configFile, "config", "", "config file")
	flag.StringVar(&resultPath, "result", "", "result file")
	flag.StringVar(&influxUrl, "influx-url", "", "InfluxDB url")
	flag.StringVar(&influxToken, "influx-token", "", "InFluxDB Token")
	flag.StringVar(&logLevel, "log", "", "log level(only 'debug' is supported right now)")
	flag.BoolVar(&exact, "exact", false, "show only accounts from config file")

	flag.Parse()

	if exact && configFile == "" {
		flag.PrintDefaults()
		os.Exit(1)
	}

	if keyID != "" {
		os.Setenv("AWS_ACCESS_KEY_ID", keyID)
	}

	if secretKey != "" {
		os.Setenv("AWS_SECRET_ACCESS_KEY", secretKey)
	}

	if region != "" {
		os.Setenv("AWS_REGION", region)
	}

	if date == "" {
		yesterday := time.Now().AddDate(0, 0, -1)
		date = yesterday.Format("2006-01-02")
	}

	if influxUrl != "" {
		fmt.Println("Output influx selected")
		client = influxdb2.NewClient(influxUrl, influxToken)
		writeAPI = client.WriteAPIBlocking("adot", "metrics")
	}

	resultFile = os.Stdout
	if resultPath != "" {
		_ = os.Remove(resultPath)
		resultFile, err = os.OpenFile(resultPath, os.O_CREATE|os.O_WRONLY, 0666)
		if err != nil {
			log.Fatalf("failed opening file: %s", err)
		}
	}

	groupDefinitionLabels = make([]groupDefinitionLabel, 2)
	groupDefinitionLabels[0] = groupDefinitionLabel{
		GroupId: "SERVICE",
		Label:   "service_name",
	}
	groupDefinitionLabels[1] = groupDefinitionLabel{
		GroupId: "LINKED_ACCOUNT",
		Label:   "account_id",
	}

	filter = types.Expression{}

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
		return c, fmt.Errorf("can't logroupDefinitionLabelsad config: %v", err)
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
	if !reflect.DeepEqual(types.Expression{}, c.Filter) {
		filter = c.Filter
	}
	if len(c.GlobalTags) != 0 {
		globalTags = c.GlobalTags
	}

	if len(c.Group) == 1 {
		groupDefinitionLabels[1] = c.Group[0]
	}

	if len(c.Group) >= 2 {
		groupDefinitionLabels[0] = c.Group[0]
		groupDefinitionLabels[1] = c.Group[1]
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

	caui := costexplorer.GetCostAndUsageInput{
		Granularity: "DAILY",
		Metrics:     []string{"UnblendedCost"},
		GroupBy: []types.GroupDefinition{{
			Type: "DIMENSION",
			Key:  &groupDefinitionLabels[0].GroupId,
		}, {
			Type: "DIMENSION",
			Key:  &groupDefinitionLabels[1].GroupId,
		}},
		TimePeriod: &types.DateInterval{
			Start: &date,
			End:   &end,
		},
	}
	if !reflect.DeepEqual(types.Expression{}, filter) {
		caui.Filter = &filter
	}

	data, err := ce.GetCostAndUsage(ctx, &caui)

	if err != nil {
		return nil, fmt.Errorf("failed to do request, %v", err)
	}

	debugLogger.Printf("collected %v items from AWS\n", len(data.ResultsByTime[0].Groups))
	return &data.ResultsByTime, err
}

func cleanString(s string) string {
	return strings.Replace(strings.Replace(s, " ", "_", -1), "_-_", "_", -1)
}

func getServiceCost(results *[]types.ResultByTime) []serviceCost {
	sc := []serviceCost{}
	t, _ := time.Parse("2006-01-02", date)
	timestamp := strconv.FormatInt(t.UnixNano(), 10)

	for _, timePeriod := range *results {
		for _, group := range timePeriod.Groups {
			sc = append(sc, serviceCost{
				SecondGroupKey: cleanString(group.Keys[1]),
				FirstGroupKey:  cleanString(group.Keys[0]),
				ServiceCost:    *group.Metrics["UnblendedCost"].Amount,
				Timestamp:      timestamp,
			})
		}
	}
	return sc
}

func printToOutput(tpl string, params ...string) {
	tmp := make([]interface{}, len(params))
	for i, val := range params {
		tmp[i] = val
	}
	if influxUrl != "" {
		line := fmt.Sprintf(tpl, tmp...)
		err := writeAPI.WriteRecord(context.Background(), line)
		if err != nil {
			panic(err)
		}
	} else {
		fmt.Fprintf(resultFile, tpl+"\n", tmp...)
	}
}

func printInfluxLineProtocol(servicesFromAWS []serviceCost, c Config) {
	debugLogger.Printf("printing result in Influx Line Protocol to %s\n", resultFile.Name())
	globalTagsString := getStringWithTags(globalTags)
	if len(c.Accounts) == 0 || groupDefinitionLabels[1].GroupId != "LINKED_ACCOUNT" {
		for _, s := range servicesFromAWS {
			printToOutput(
				"aws_cost,%v=%v,%v=%v%v cost=%v %v",
				groupDefinitionLabels[1].Label,
				s.SecondGroupKey,
				groupDefinitionLabels[0].Label,
				s.FirstGroupKey,
				globalTagsString,
				s.ServiceCost,
				s.Timestamp)
		}
	} else {
		for _, s := range servicesFromAWS {
			if exact {
				if ok, accountName, accountTags := checkElementInArray(c, s.SecondGroupKey); ok {
					printToOutput(
						"aws_cost,%v=%v,account_name=%v,%v=%v%v cost=%v %v",
						groupDefinitionLabels[1].Label,
						s.SecondGroupKey,
						accountName,
						groupDefinitionLabels[0].Label,
						s.FirstGroupKey,
						globalTagsString+accountTags,
						s.ServiceCost,
						s.Timestamp)
				}
			} else {
				if ok, accountName, accountTags := checkElementInArray(c, s.SecondGroupKey); ok {
					printToOutput(
						"aws_cost,%v=%v,account_name=%v,%v=%v%v cost=%v %v",
						groupDefinitionLabels[1].Label,
						s.SecondGroupKey,
						accountName,
						groupDefinitionLabels[0].Label,
						s.FirstGroupKey,
						globalTagsString+accountTags,
						s.ServiceCost,
						s.Timestamp)
				} else {
					printToOutput(
						"aws_cost,%v=%v,account_name=%v,%v=%v%v cost=%v %v",
						groupDefinitionLabels[1].Label,
						s.SecondGroupKey,
						accountName,
						groupDefinitionLabels[0].Label,
						s.FirstGroupKey,
						globalTagsString,
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

func getStringWithTags(inputTags map[string]string) string {
	tags := ""
	for tag := range inputTags {
		tags += fmt.Sprintf(",%s=%s", tag, inputTags[tag])
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

	if influxUrl != "" {
		defer client.Close()
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
