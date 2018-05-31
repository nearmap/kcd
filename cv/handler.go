package cv

import (
	"encoding/json"
	"html/template"
	"io"
	"net/http"

	"github.com/golang/glog"
	k8s "github.com/nearmap/cvmanager/gok8s/workload"
	"github.com/pkg/errors"
)

func genCVHTML(w io.Writer, cvs []*k8s.Resource) error {
	t := template.Must(template.New("cvList").Parse(cvListHTML))
	err := t.Execute(w, cvs)
	if err != nil {
		return errors.Wrap(err, "Failed to generate template of CV list")
	}
	return nil
}

func genCV(w io.Writer, cvs []*k8s.Resource) error {
	bytes, err := json.Marshal(cvs)
	if err != nil {
		return errors.Wrap(err, "Failed to convert cv list to json")
	}

	w.Write(bytes)
	return nil
}

// AllContainerVersions provides details of current container version of all workload
// managed by CV managed resource.
// supports json and html format specified via typ.
func AllContainerVersions(w io.Writer, typ string, k8sProvider *k8s.Provider) error {
	cvsList, err := k8sProvider.AllResources()
	if err != nil {
		return errors.Wrap(err, "failed to generate list of cv resources")
	}

	switch typ {
	case "html":
		return genCVHTML(w, cvsList)
	case "json":
		return genCV(w, cvsList)
	default:
		return errors.New("Unsupported format requested")
	}

	return nil
}

// NewCVHandler is web handler to generate HTML tabular text listing all status of all CV managed resource
// as represented by Workloads
func NewCVHandler(k8sProvider *k8s.Provider) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		typ := r.URL.Query().Get("format")
		if typ != "json" && typ != "html" {
			typ = "json"
		}

		err := AllContainerVersions(w, typ, k8sProvider)
		if err != nil {
			glog.Errorf("failed to get workload list %v", err)
			http.Error(w, http.StatusText(http.StatusInternalServerError), http.StatusInternalServerError)
		}
		// // Allow origin so can easily be used by monitoring
		// w.Header().Set("Access-Control-Allow-Origin", "*")
		// w.Header().Set("X-Frame-Options", "ALLOWALL")
	}
}
