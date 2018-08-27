package kcd

import (
	"encoding/json"
	"html/template"
	"io"
	"net/http"
	"time"

	"github.com/golang/glog"
	"github.com/nearmap/kcd/resource"
	"github.com/pkg/errors"
	"goji.io/pat"
)

func genCVHTML(w io.Writer, resources []*resource.Resource, namespace string, reload bool) error {
	t := template.Must(template.New("kcdList").Parse(kcdListHTML))
	data := struct {
		Resources []*resource.Resource
		Namespace string
		Reload    bool
	}{
		Resources: resources,
		Namespace: namespace,
		Reload:    reload,
	}
	err := t.Execute(w, data)
	if err != nil {
		return errors.Wrap(err, "Failed to generate template of CV list")
	}
	return nil
}

func genCV(w io.Writer, resources []*resource.Resource) error {
	bytes, err := json.Marshal(resources)
	if err != nil {
		return errors.Wrap(err, "Failed to convert kcd list to json")
	}
	w.Write(bytes)
	return nil
}

// AllKCDs provides details of current container version of all workload
// managed by CV managed resource.
// supports json and html format specified via typ.
// If namespace is empty then all namespaces are returned.
func AllKCDs(w io.Writer, typ, namespace string, resourceProvider resource.Provider, reload bool) error {
	kcdsList, err := resourceProvider.AllResources(namespace)
	if err != nil {
		return errors.Wrap(err, "failed to generate list of kcd resources")
	}

	switch typ {
	case "html":
		return genCVHTML(w, kcdsList, namespace, reload)
	case "json":
		return genCV(w, kcdsList)
	default:
		return errors.New("Unsupported format requested")
	}
}

// NewAllResourceHandler is web handler that generates JSON and HTML listings of all
// KCD managed resources.
func NewAllResourceHandler(resourceProvider resource.Provider) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		q := r.URL.Query()
		typ := q.Get("format")
		if typ != "json" && typ != "html" {
			typ = "json"
		}
		reload := q.Get("reload")

		err := AllKCDs(w, typ, "", resourceProvider, reload == "true")
		if err != nil {
			glog.Errorf("failed to get workload list %v", err)
			http.Error(w, http.StatusText(http.StatusInternalServerError), http.StatusInternalServerError)
		}
		// Allow origin so can easily be used by monitoring
		// w.Header().Set("Access-Control-Allow-Origin", "*")
		// w.Header().Set("X-Frame-Options", "ALLOWALL")
	}
}

// NewResourceHandler is web handler that generates JSON and HTML listings of all
// KCD managed resources.
func NewResourceHandler(resourceProvider resource.Provider) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		namespace := pat.Param(r, "namespace")

		q := r.URL.Query()
		typ := q.Get("format")
		if typ != "json" && typ != "html" {
			typ = "json"
		}
		reload := q.Get("reload")

		err := AllKCDs(w, typ, namespace, resourceProvider, reload == "true")
		if err != nil {
			glog.Errorf("failed to get workload list %v", err)
			http.Error(w, http.StatusText(http.StatusInternalServerError), http.StatusInternalServerError)
		}
		// Allow origin so can easily be used by monitoring
		// w.Header().Set("Access-Control-Allow-Origin", "*")
		// w.Header().Set("X-Frame-Options", "ALLOWALL")
	}
}

// NewResourceUpdateHandler is a web handler that performs status updates of KCD
// managed resources.
func NewResourceUpdateHandler(resourceProvider resource.Provider) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		name := pat.Param(r, "name")
		namespace := pat.Param(r, "namespace")

		q := r.URL.Query()
		status := q.Get("status")

		_, err := resourceProvider.UpdateStatus(namespace, name, "", status, time.Now().UTC())
		if err != nil {
			glog.Error("failed to update resource for name=%s, error=%+v", name, err)
			http.Error(w, http.StatusText(http.StatusInternalServerError), http.StatusInternalServerError)
		}
	}
}
