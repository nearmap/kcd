package signals

import (
	"os"
	"os/signal"
)

// SetupSignalHandler registered for SIGINT. A stop channel is returned
// which is closed on one of these signals.
func SetupSignalHandler() chan struct{} {
	stop := make(chan struct{})
	c := make(chan os.Signal, 2)
	signal.Notify(c, os.Interrupt)
	go func() {
		<-c
		close(stop)
		<-c
		os.Exit(1) // second signal. Exit directly.
	}()
	return stop
}

// SetupTwoWaySignalHandler registered for SIGINT. It is a
// two way signal handler
func SetupTwoWaySignalHandler() chan os.Signal {
	stop := make(chan os.Signal)
	signal.Notify(stop, os.Interrupt)
	go func() {
		<-stop
		close(stop)
	}()
	return stop
}
