package history

import (
	"net/http"

	"github.com/golang/glog"
	"goji.io/pat"
)

// NewHandler is web handler to return history of workload updates
// as performed by kcd
func NewHandler(provider Provider) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		name := pat.Param(r, "name")
		ns := r.URL.Query().Get("namespace")
		if ns == "" {
			ns = "default"
		}

		msg, err := provider.History(ns, name)
		if err != nil {
			glog.Errorf("Failed to get history of workload %v", err)
			http.Error(w, http.StatusText(http.StatusInternalServerError), http.StatusInternalServerError)
		}
		w.Write([]byte(msg))
	}
}
