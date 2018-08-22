/*
 go build -o sceforecast
 ./sceforecast

 The code runs once every a predefined hour period and checks the likelyhood of
 an event in the next predefined number of days (MAX forecast is 6 days)
 (e.g., runs once every 24 hours and checks the forecast for the next 5 days)

 for more details on CPP visit https://www.sce.com/NR/sc3/tm2/pdf/ce300.pdf

 If there is a chance an event will occur:
  it uses AWS SNS to notify everyone on the user topic
 If the code fails (in case SCE changes their website):
  it will notify everyone on the developer topic
*/
package main

import (
	"encoding/json"
	"errors"
	"log"
	"net/http"
	"os"
	"strconv"
	"time"

	"github.com/aws/aws-sdk-go/aws"
	"github.com/aws/aws-sdk-go/aws/session"
	"github.com/aws/aws-sdk-go/service/sns"
	"golang.org/x/net/html"
)

// configuration file path and variable
const confPath = "./config.json"

var config Config

type Config struct {
	Usrtopic            string //AWS user SNS topic
	Usrtopicregion      string //AWS user SNS topic region
	Devtopic            string //AWS developer SNS topic
	Devtopicregion      string //AWS developer SNS topic region
	Disablenotification bool   //set true to disable SNS notification
	Period              int    //Overall period in hours to repeat notification (e.g., 24 hours)
	Forecastdays        int    //Number of days to get the forecast for MAX = 6
	Sceurl              string //URL for SCE  forecast
}

func main() {
	httpclient := &http.Client{Timeout: 10 * time.Second}

Main:
	for {
		// reload configuration in case something changed
		configure()
		start := time.Now()
		// request and parse the front page
		resp, err := httpclient.Get(config.Sceurl)
		if err != nil {
			notify("Error: failed to GET main website from SCE: "+err.Error(), config.Devtopic, config.Devtopicregion)
			break Main
		}
		defer resp.Body.Close()
		if resp.StatusCode != 200 {
			notify("Error: GET status code is: "+strconv.Itoa(resp.StatusCode), config.Devtopic, config.Devtopicregion)
			break Main
		}

		log.Println("about to parse website")
		z := html.NewTokenizer(resp.Body)
	Parse:
		for {
			tt := z.Next()
			switch tt {
			case html.ErrorToken:
				// End of the document, we're done
				break Parse
			case html.StartTagToken:
				t := z.Token()
				for _, a := range t.Attr {
					if a.Val == "rich-table EvtRTPTable" {
						parseTable(z)
					}
				}
			}
		}
		// run this code once every Period hours (e.g., 24 hours)
		elapsed := time.Since(start)
		time.Sleep(time.Duration(config.Period)*time.Hour - elapsed)
	}
}

// parseTable parses Table for weather events and notifies SNS for each EXTREMELY HOT SUMMER WEEKDAY row
func parseTable(z *html.Tokenizer) {
	var daycount int
	for {
		tt := z.Next()
		switch tt {
		case html.ErrorToken:
			// End of the document, we're done
			return
		case html.StartTagToken:
			t := z.Token()
			if t.Data == "td" {
				for _, a := range t.Attr {
					if a.Val == "rich-table-cell rich-table-cell-first" {
						z.Next()
						t = z.Token()
						if parseDate(t.Data) {
							daycount++
							if daycount > config.Forecastdays {
								return
							}
							s := parseRows(z)
							log.Println(t.Data, s)
							if s == "EXTREMELY HOT SUMMER WEEKDAY" {
								notify("SCE CPP DR Event likely to happen on: "+t.Data+" from: 2:00 pm to 6:00 pm", config.Usrtopic, config.Usrtopicregion)
							}
						}
					}
				}
			}
		}
	}
}

// parseDate in the format July 16, 2018
func parseDate(t string) bool {
	_, err := time.Parse("January 02, 2006", t)
	if err != nil {
		return false
	}
	return true
}

// parseRows parses rows from temperature table and returns forecast
func parseRows(z *html.Tokenizer) string {
	for {
		tt := z.Next()
		switch tt {
		case html.ErrorToken:
			// End of the document, we're done
			return ""
		case html.StartTagToken:
			t := z.Token()
			if t.Data == "td" {
				for _, a := range t.Attr {
					if a.Val == "rich-table-cell rich-table-cell-last" {
						z.Next()
						t = z.Token()
						return t.Data
					}
				}
			}
		}
	}
}

// notify notifies someone when a DR event is about to happen using AWS SNS
func notify(msg string, topic string, region string) {
	log.Println(msg)
	if config.Disablenotification {
		log.Println("notification is disabled")
		return
	}
	sess := session.Must(session.NewSession())
	//region for AWS SNS topics
	svc := sns.New(sess, aws.NewConfig().WithRegion(region))
	params := &sns.PublishInput{
		Message:  aws.String(msg),
		TopicArn: aws.String(topic),
	}
	_, err := svc.Publish(params)
	if err != nil {
		log.Println("failed to send SNS", err)
	}
}

// configure loads configuration paramters from config file
func configure() {
	f, err := os.Open(confPath)
	if err != nil {
		log.Fatal(errors.New("Error: failed to load configuration file (./config.json). " + err.Error()))
		return
	}
	d := json.NewDecoder(f)
	err = d.Decode(&config)
	if err != nil {
		log.Fatal(errors.New("Error: failed to configure server. " + err.Error()))
		return
	}
	if config.Forecastdays > 6 {
		log.Println("Forecastdays cannot exceed 6 days, setting to 6")
		config.Forecastdays = 6
	}

}