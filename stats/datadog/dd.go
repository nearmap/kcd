// Package datadog provides stats logging using datadog.
package datadog

import (
	"fmt"
	"log"
	"net"
	"sync"
	"time"

	"github.com/DataDog/datadog-go/statsd"
)

// DataDogStats is statsd client wrapper
type DataDogStats struct {
	sync.Mutex

	client *statsd.Client
}

// New returns a DataDog stats instance that implements the Stats interface and sends
// data to the provided datadog address.
func New(address, namespace string, tags ...string) (*DataDogStats, error) {
	if address == "" {
		return &DataDogStats{}, fmt.Errorf("no address provided for stats")
	}
	address = addDefaultPort(address)
	log.Printf("Sending stats to %s with namespace %s", address, namespace)

	client, err := statsd.NewBuffered(address, 1432)
	if err != nil {
		return &DataDogStats{}, fmt.Errorf("failed to connect to statsd port at %s: %v", address, err)
	}
	client.Namespace = namespace
	client.Tags = tags

	return &DataDogStats{
		client: client,
	}, nil
}

// Close closes statsd client
func (stats *DataDogStats) Close() error {
	return stats.client.Close()
}

// IncCount is used to capture stats are are continuous and increment it by 1
func (stats *DataDogStats) IncCount(name string, tags ...string) {
	log.Printf("sending %s metric of tag %s", name, tags)
	err := stats.client.Incr(name, tags, 1.0)
	if err != nil {
		log.Printf("Error sending %s metric: %+v", name, err)
	}
}

// IncCountBy decrement stats by specified value
func (stats *DataDogStats) IncCountBy(name string, value int64, tags ...string) {
	err := stats.client.Count(name, value, tags, 1.0)
	if err != nil {
		log.Printf("Error sending %s metric: %+v", name, err)
	}
}

// DecCount decrement stats by 1
func (stats *DataDogStats) DecCount(name string, tags ...string) {
	err := stats.client.Decr(name, tags, 1.0)
	if err != nil {
		log.Printf("Error sending %s metric: %+v", name, err)
	}
}

// DecCountBy decrement stats by specified value
func (stats *DataDogStats) DecCountBy(name string, value int64, tags ...string) {
	err := stats.client.Count(name, -1*value, tags, 1.0)
	if err != nil {
		log.Printf("Error sending %s metric: %+v", name, err)
	}
}

// ElapsedTime captures the timing based on start time and current time
func (stats *DataDogStats) ElapsedTime(start time.Time, name string, tags ...string) {
	log.Printf("sending %s metric of tag %s", name, tags)
	elapsed := time.Since(start)
	stats.Duration(elapsed, name, tags...)
}

// Duration captures the duration as timing stats
func (stats *DataDogStats) Duration(duration time.Duration, name string, tags ...string) {
	// time.Nanosecond / time.Milisecond below
	// gives us the ratio between the two constants, which we multiply by
	// duration in nanoseconds to convert to duration in miliseconds.
	err := stats.client.Timing(name+".timing", duration, tags, 1.0)
	if err != nil {
		log.Printf("Error sending timing metric %s: %+v", name, err)
	}
}

// Gauge generates Gauge
func (stats *DataDogStats) Gauge(name string, value int64, tags ...string) {
	err := stats.client.Gauge(name, float64(value), tags, 1.0)
	if err != nil {
		log.Printf("Error sending %s gauge: %+v", name, err)
	}
}

// Histogram generates Histogram
func (stats *DataDogStats) Histogram(name string, value int64, tags ...string) {
	err := stats.client.Histogram(name, float64(value), tags, 1.0)
	if err != nil {
		log.Printf("Error sending histogram %s: %+v", name, err)
	}
}

// Event contains details of an activity that was captured and
// notifies it to stats backend
func (stats *DataDogStats) Event(title, mesg, aggKey, typ string, timestamp time.Time, tags ...string) {
	log.Printf("sending %s event metric of value %s and tag %s", title, mesg, tags)
	event := &statsd.Event{
		Title:          fmt.Sprintf("%s.event", title),
		Text:           mesg,
		Timestamp:      timestamp,
		AggregationKey: aggKey,
		Tags:           tags,
	}
	switch typ {
	case "info":
		event.AlertType = statsd.Info
		break
	case "error":
		event.AlertType = statsd.Error
		break
	case "warning":
		event.AlertType = statsd.Warning
		break
	case "success":
		event.AlertType = statsd.Success
		break
	default:
		log.Printf("Event type %s is unrecognized. Failed to send event %s \n", typ, title)
		return
	}
	err := stats.client.Event(event)
	if err != nil {
		log.Printf("Error sending event %s: %+v", title, err)
	}
}

// ServiceCheck notifies monitoring backend about the status of
// service (Ok/Warning/Critical/Unknow).
func (stats *DataDogStats) ServiceCheck(name, mesg string, status int, timestamp time.Time, tags ...string) {
	log.Printf("sending %s service check status %d and tag %s", name, status, tags)
	sc := &statsd.ServiceCheck{
		Name:      fmt.Sprintf("%s.sc", name),
		Message:   mesg,
		Timestamp: timestamp,
		Tags:      tags,
	}
	switch status {
	case 0:
		sc.Status = statsd.Ok
		break
	case 1:
		sc.Status = statsd.Warn
		break
	case 2:
		sc.Status = statsd.Critical
		break
	case 4:
		sc.Status = statsd.Unknown
		break
	default:
		log.Printf("Service check status %v is unrecognized. Failed to send service check status %s \n", status, name)
		return
	}
	err := stats.client.ServiceCheck(sc)
	if err != nil {
		log.Printf("Error sending event %s: %+v", name, err)
	}
}

// addDefaultPort adds default statsd port 8125 if port is not already specified
// to a given hostname
func addDefaultPort(host string) string {
	if h, p, err := net.SplitHostPort(host); err != nil {
		host = fmt.Sprintf("%s:8125", host)
	} else if p == "" {
		host = fmt.Sprintf("%s:8125", h)
	}
	return host
}
