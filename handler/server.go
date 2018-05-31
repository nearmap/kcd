package handler

import (
	"context"
	"fmt"
	"net/http"
	"time"

	"github.com/golang/glog"
	"github.com/nearmap/cvmanager/cv"
	k8s "github.com/nearmap/cvmanager/gok8s/workload"
	"github.com/nearmap/cvmanager/history"
	goji "goji.io"
	"goji.io/pat"
)

// StaticContentHandler returns a HandlerFunc that writes the given content
// to the response.
func StaticContentHandler(content string) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if _, err := w.Write([]byte(content)); err != nil {
			http.Error(w, http.StatusText(http.StatusInternalServerError), http.StatusInternalServerError)
		}
	}
}

// NewServer creates and starts an http server to serve alive and deployment status endpoints
// if server fails to start then, stop channel is closed notifying all listeners to the channel
func NewServer(port int, version string,
	k8sProvider *k8s.Provider, historyProvider history.Provider, stopCh chan struct{}) {

	mux := goji.NewMux()
	mux.Handle(pat.Get("/alive"), StaticContentHandler("alive"))
	mux.Handle(pat.Get("/version"), StaticContentHandler(version))
	mux.Handle(pat.Get("/v1/cv/workloads"), cv.NewCVHandler(k8sProvider))
	mux.Handle(pat.Get("/v1/cv/workloads/:name"), history.NewHandler(historyProvider))

	srv := &http.Server{
		Addr:         fmt.Sprintf(":%d", port),
		Handler:      mux,
		ReadTimeout:  10 * time.Second,
		WriteTimeout: 1 * time.Minute,
	}
	go func() {
		if err := srv.ListenAndServe(); err != nil {
			if err.Error() != "http: Server closed" {
				glog.V(2).Infof("Server error during ListenAndServe: %v", err)
				close(stopCh)
			}
		}
	}()

	<-stopCh
	glog.V(2).Infof("Shutting down http server")

	ctx, _ := context.WithTimeout(context.Background(), 5*time.Second)
	srv.Shutdown(ctx)
	glog.V(1).Infof("Server gracefully stopped")
}
