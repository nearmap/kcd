package kcd

import (
	"encoding/json"
	"html/template"
	"io"
	"net/http"

	"github.com/golang/glog"
	k8s "github.com/nearmap/kcd/gok8s/workload"
	"github.com/pkg/errors"
)

func genCVHTML(w io.Writer, kcds []*k8s.Resource, namespace string) error {
	t := template.Must(template.New("kcdList").Parse(kcdListHTML))
	data := struct {
		KCDs      []*k8s.Resource
		Namespace string
	}{
		KCDs:      kcds,
		Namespace: namespace,
	}
	err := t.Execute(w, data)
	if err != nil {
		return errors.Wrap(err, "Failed to generate template of CV list")
	}
	return nil
}

func genCV(w io.Writer, kcds []*k8s.Resource) error {
	bytes, err := json.Marshal(kcds)
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
func AllKCDs(w io.Writer, typ, namespace string, k8sProvider *k8s.Provider) error {
	kcdsList, err := k8sProvider.AllResources(namespace)
	if err != nil {
		return errors.Wrap(err, "failed to generate list of kcd resources")
	}

	switch typ {
	case "html":
		return genCVHTML(w, kcdsList, namespace)
	case "json":
		return genCV(w, kcdsList)
	default:
		return errors.New("Unsupported format requested")
	}
}

// NewCVHandler is web handler to generate HTML tabular text listing all status of all CV managed resource
// as represented by Workloads
func NewCVHandler(k8sProvider *k8s.Provider) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		q := r.URL.Query()
		typ := q.Get("format")
		if typ != "json" && typ != "html" {
			typ = "json"
		}
		namespace := q.Get("namespace")

		err := AllKCDs(w, typ, namespace, k8sProvider)
		if err != nil {
			glog.Errorf("failed to get workload list %v", err)
			http.Error(w, http.StatusText(http.StatusInternalServerError), http.StatusInternalServerError)
		}
		// Allow origin so can easily be used by monitoring
		// w.Header().Set("Access-Control-Allow-Origin", "*")
		// w.Header().Set("X-Frame-Options", "ALLOWALL")
	}
}
