// Get Enphase Envoy Solar production data into InfluxDB

// For options:
// > influxEnvoyStats -h

// API path used by the webpage provided by Envoy is e.g.:
//  http://envoy/production.json?details=1

// David Lamb
// 2018-12

package main

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"io/ioutil"
	"net/http"
	"time"

	influxdb2 "github.com/influxdata/influxdb-client-go/v2"
	"github.com/influxdata/influxdb-client-go/v2/api"
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

type EnvoyMonitor struct {
	envoyHostPtr       *string
	influxAddrPtr      *string
	dbOrgPtr           *string
	dbBucketPtr        *string
	dbUserPtr          *string
	dbPwPtr            *string
	measurementNamePtr *string
	loopIntervalPtr    *time.Duration

	envoyClient  http.Client
	influxClient influxdb2.Client
	writeAPI     api.WriteAPIBlocking
}

func main() {
	envoyHostPtr := flag.String("e", "envoy", "IP or hostname of Envoy")
	influxAddrPtr := flag.String("dba", "http://localhost:8086", "InfluxDB connection address")
	dbOrgPtr := flag.String("dbo", "solar", "Influx database org to put readings in")
	dbBucketPtr := flag.String("dbn", "solar", "Influx database name to put readings in")
	dbUserPtr := flag.String("dbu", "user", "DB username")
	dbPwPtr := flag.String("dbp", "pw", "DB password")
	measurementNamePtr := flag.String("m", "readings", "Influx measurement name customisation (table name equivalent)")
	loopIntervalPtr := flag.Duration("loop", 0, "Loop interval (0 means poll once and exit)")
	flag.Parse()

	monitor := EnvoyMonitor{
		envoyHostPtr:       envoyHostPtr,
		influxAddrPtr:      influxAddrPtr,
		dbOrgPtr:           dbOrgPtr,
		dbBucketPtr:        dbBucketPtr,
		dbUserPtr:          dbUserPtr,
		dbPwPtr:            dbPwPtr,
		measurementNamePtr: measurementNamePtr,
		loopIntervalPtr:    loopIntervalPtr,

		envoyClient: http.Client{
			Timeout: time.Second * 2, // Maximum of 2 secs
		},
	}

	defer func() {
		if monitor.influxClient != nil {
			monitor.influxClient.Close()
		}
	}()

	if *loopIntervalPtr > 0 {
		ticker := time.NewTicker(*loopIntervalPtr)
		quit := make(chan struct{})
		//go func() {
		for {
			select {
			case <-ticker.C:
				err := poll(&monitor)
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
		err := poll(&monitor)
		if err != nil {
			panic(err)
		}
	}
}

func poll(monitor *EnvoyMonitor) (err error) {
	prodReadings, consumptionReadings, err := pollEnvoy(monitor)
	if err != nil {
		return
	}

	err = writeToInflux(monitor, prodReadings, consumptionReadings)
	if err != nil {
		return
	}

	return
}

func pollEnvoy(monitor *EnvoyMonitor) (prodReadings Eim, consumptionReadings []Eim, err error) {
	prodReadings = Eim{}
	consumptionReadings = nil

	envoyUrl := "http://" + *monitor.envoyHostPtr + "/production.json?details=1"
	req, err := http.NewRequest(http.MethodGet, envoyUrl, nil)
	if err != nil {
		return
	}

	resp, err := monitor.envoyClient.Do(req)
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

func writeToInflux(monitor *EnvoyMonitor, prodReadings Eim, consumptionReadings []Eim) (err error) {
	// Connect to influxdb specified in commandline arguments
	if monitor.influxClient == nil {
		monitor.influxClient = influxdb2.NewClient(*monitor.influxAddrPtr, fmt.Sprintf("%s:%s", *monitor.dbUserPtr, *monitor.dbPwPtr))
		monitor.writeAPI = monitor.influxClient.WriteAPIBlocking(*monitor.dbOrgPtr, *monitor.dbBucketPtr)
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

		pt := influxdb2.NewPoint(
			*monitor.measurementNamePtr,
			tags,
			fields,
			createdTime,
		)

		err = monitor.writeAPI.WritePoint(context.Background(), pt)
		if err != nil {
			return
		}
	}

	return
}
