package stats

import (
	"log"
	"time"
)

type Stats interface {
	IncCount(name string, tags ...string)

	ServiceCheck(name, mesg string, status int, timestamp time.Time, tags ...string)

	Event(title, mesg, aggKey, typ string, timestamp time.Time, tags ...string)
}

type FakeStats struct {
}

func NewFake() *FakeStats {
	log.Printf("Fake stats client in use. Stats are not sent!")
	return &FakeStats{}
}

func (fs *FakeStats) IncCount(name string, tags ...string) {
}

func (fs *FakeStats) ServiceCheck(name, mesg string, status int, timestamp time.Time, tags ...string) {
}

func (fs *FakeStats) Event(title, mesg, aggKey, typ string, timestamp time.Time, tags ...string) {
}
