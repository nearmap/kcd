package handler

import (
	"context"
	"log"
	"net/http"
	"time"

	"github.com/nearmap/cvmanager/cv"
	clientset "github.com/nearmap/cvmanager/gok8s/client/clientset/versioned"
	goji "goji.io"
	"goji.io/pat"
	"k8s.io/client-go/kubernetes"
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

func NewServer(cs kubernetes.Interface, customCS clientset.Interface, stopCh <-chan struct{}) {

	mux := goji.NewMux()
	mux.Handle(pat.Get("/alive"), StaticContentHandler("alive"))
	mux.Handle(pat.Get("/v1/cv"), cv.NewCVHandler(cs, customCS))

	srv := &http.Server{
		Addr:         ":8081",
		Handler:      mux,
		ReadTimeout:  10 * time.Second,
		WriteTimeout: 2 * time.Minute,
	}
	go func() {
		if err := srv.ListenAndServe(); err != nil {
			if err.Error() != "http: Server closed" {
				log.Printf("Server error during ListenAndServe: %v", err)
				//stopCh <- os.Interrupt
			}
		}
	}()

	<-stopCh
	log.Print("Shutting down http server")

	ctx, _ := context.WithTimeout(context.Background(), 5*time.Second)
	srv.Shutdown(ctx)
	log.Print("Server gracefully stopped")
}
