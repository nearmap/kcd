package history

import (
	"log"
	"net/http"

	"github.com/nearmap/cvmanager/stats"
	"goji.io/pat"
	"k8s.io/client-go/kubernetes"
)

// NewHandler is web handler to return history of workload updates
// as performed by cvmanager
func NewHandler(cs kubernetes.Interface) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {

		p := NewProvider(cs, stats.NewFake())
		name := pat.Param(r, "name")
		ns := r.URL.Query().Get("namespace")

		msg, err := p.History(ns, name)
		if err != nil {
			log.Printf("failed to get history of workload %v", err)
			http.Error(w, http.StatusText(http.StatusInternalServerError), http.StatusInternalServerError)
		}
		w.Write([]byte(msg))
	}
}
