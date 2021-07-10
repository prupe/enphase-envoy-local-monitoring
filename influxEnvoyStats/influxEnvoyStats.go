// Get Enphase Envoy Solar production data into InfluxDB

// For options:
// > influxEnvoyStats -h

// API path used by the webpage provided by Envoy is e.g.:
//  http://envoy/production.json?details=1

// David Lamb
// 2018-12

package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"github.com/influxdata/influxdb/client/v2"
	"io/ioutil"
	"net/http"
	"time"
)

type EnvoyAPIMeasurement struct {
	Production  json.RawMessage
	Consumption json.RawMessage
	Storage     json.RawMessage
}

type Inverters struct {
	ActiveCount int
}

type Eim struct {
	MeasurementType  string
	ReadingTime      int64
	WNow             float64
	WhLifetime       float64
	VarhLeadLifetime float64
	VarhLagLifetime  float64
	VahLifetime      float64
	RmsCurrent       float64
	RmsVoltage       float64
	ReactPwr         float64
	ApprntPwr        float64
	PwrFactor        float64
	WhToday          float64
	WhLastSevenDays  float64
	VahToday         float64
	VarhLeadToday    float64
	VarhLagToday     float64
}

type Context struct {
	envoyHostPtr       *string
	influxAddrPtr      *string
	dbNamePtr          *string
	dbUserPtr          *string
	dbPwPtr            *string
	measurementNamePtr *string
	loopIntervalPtr    *time.Duration

	envoyClient  http.Client
	influxClient client.HTTPClient
}

func main() {
	envoyHostPtr := flag.String("e", "envoy", "IP or hostname of Envoy")
	influxAddrPtr := flag.String("dba", "http://localhost:8086", "InfluxDB connection address")
	dbNamePtr := flag.String("dbn", "solar", "Influx database name to put readings in")
	dbUserPtr := flag.String("dbu", "user", "DB username")
	dbPwPtr := flag.String("dbp", "pw", "DB password")
	measurementNamePtr := flag.String("m", "readings", "Influx measurement name customisation (table name equivalent)")
	loopIntervalPtr := flag.Duration("loop", 0, "Loop interval (0 means poll once and exit)")
	flag.Parse()

	context := Context{
		envoyHostPtr:       envoyHostPtr,
		influxAddrPtr:      influxAddrPtr,
		dbNamePtr:          dbNamePtr,
		dbUserPtr:          dbUserPtr,
		dbPwPtr:            dbPwPtr,
		measurementNamePtr: measurementNamePtr,
		loopIntervalPtr:    loopIntervalPtr,

		envoyClient: http.Client{
			Timeout: time.Second * 2, // Maximum of 2 secs
		},
	}

	defer func() {
		if context.influxClient != nil {
			context.influxClient.Close()
		}
	}()

	if *loopIntervalPtr > 0 {
		ticker := time.NewTicker(*loopIntervalPtr)
		quit := make(chan struct{})
		//go func() {
		for {
			select {
			case <-ticker.C:
				err := poll(&context)
				if err != nil {
					fmt.Printf("Error: %s\n", err)
				}
			case <-quit:
				ticker.Stop()
				return
			}
		}
		//}()
	} else {
		err := poll(&context)
		if err != nil {
			panic(err)
		}
	}
}

func poll(context *Context) (err error) {
	prodReadings, consumptionReadings, err := pollEnvoy(context)
	if err != nil {
		return
	}

	err = writeToInflux(context, prodReadings, consumptionReadings)
	if err != nil {
		return
	}

	return
}

func pollEnvoy(context *Context) (prodReadings Eim, consumptionReadings []Eim, err error) {
	prodReadings = Eim{}
	consumptionReadings = nil

	envoyUrl := "http://" + *context.envoyHostPtr + "/production.json?details=1"
	req, err := http.NewRequest(http.MethodGet, envoyUrl, nil)
	if err != nil {
		return
	}

	resp, err := context.envoyClient.Do(req)
	if err != nil {
		return
	}

	jsonData, err := ioutil.ReadAll(resp.Body)
	if err != nil {
		return
	}

	var apiJsonObj struct {
		Production  json.RawMessage
		Consumption json.RawMessage
		Storage     json.RawMessage
	}
	err = json.Unmarshal(jsonData, &apiJsonObj)
	if err != nil {
		return
	}

	inverters := Inverters{}
	prodReadings = Eim{}
	productionObj := []interface{}{&inverters, &prodReadings}
	err = json.Unmarshal(apiJsonObj.Production, &productionObj)
	if err != nil {
		return
	}

	fmt.Printf("%d production: %.3f\n", prodReadings.ReadingTime, prodReadings.WNow)

	consumptionReadings = []Eim{}
	err = json.Unmarshal(apiJsonObj.Consumption, &consumptionReadings)
	if err != nil {
		return
	}

	for _, eim := range consumptionReadings {
		fmt.Printf("%d %s: %.3f\n", eim.ReadingTime, eim.MeasurementType, eim.WNow)
	}

	return
}

func writeToInflux(context *Context, prodReadings Eim, consumptionReadings []Eim) (err error) {
	bp, err := client.NewBatchPoints(client.BatchPointsConfig{
		Database:  *context.dbNamePtr,
		Precision: "s",
	})
	if err != nil {
		return
	}

	readings := append(consumptionReadings, prodReadings)
	for _, reading := range readings {
		tags := map[string]string{
			"type": reading.MeasurementType,
		}
		fields := map[string]interface{}{
			"watts": reading.WNow,
		}
		createdTime := time.Unix(reading.ReadingTime, 0)
		if err != nil {
			return
		}

		var pt *client.Point
		pt, err = client.NewPoint(
			*context.measurementNamePtr,
			tags,
			fields,
			createdTime,
		)
		if err != nil {
			return
		}

		bp.AddPoint(pt)
	}

	// Connect to influxdb specified in commandline arguments
	if context.influxClient == nil {
		context.influxClient, err = client.NewHTTPClient(client.HTTPConfig{
			Addr:     *context.influxAddrPtr,
			Username: *context.dbUserPtr,
			Password: *context.dbPwPtr,
		})
		if err != nil {
			return
		}
	}

	// Write the batch
	err = context.influxClient.Write(bp)
	if err != nil {
		return
	}

	return
}
