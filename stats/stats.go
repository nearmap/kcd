package stats

import (
	"log"
	"time"
)

// Stats allows sending increment, service check and event message
// to statsd backend
type Stats interface {
	// IncCount captures the continuous stats (identified by name)
	// and delivers it to statsd backend
	IncCount(name string, tags ...string)

	// ServiceCheck captures the status of a service when a change in the
	// service status was notified
	ServiceCheck(name, mesg string, status int, timestamp time.Time, tags ...string)

	// Event contains details of an activity that was captured and
	// notifies it to stats backend
	Event(title, mesg, aggKey, typ string, timestamp time.Time, tags ...string)
}

// FakeStats implements Stats interface wherein for instead of sending the stats
// backend, it just logs the messages.
type FakeStats struct {
}

// NewFake return an instance of FakeStats
func NewFake() *FakeStats {
	log.Printf("Fake stats client in use. Stats are not sent!")
	return &FakeStats{}
}

// IncCount is used to capture stats are are continuous
func (fs *FakeStats) IncCount(name string, tags ...string) {
	log.Printf("Stats: %s is notified, tags are %s", name, tags)
}

// ServiceCheck logs the status of service
func (fs *FakeStats) ServiceCheck(name, mesg string, status int, timestamp time.Time, tags ...string) {
	log.Printf("Stats: Service Check %s with status %d is received with message %s @ time %s, tags are %s",
		name, status, mesg, timestamp.String(), tags)
}

// Event captures the details of events in log
func (fs *FakeStats) Event(title, mesg, aggKey, typ string, timestamp time.Time, tags ...string) {
	log.Printf("Stats: Event %s of type %s is received with message %s @ time %s, tags are %s",
		title, typ, mesg, timestamp.String(), tags)

}
