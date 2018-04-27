package cv

import (
	"encoding/json"
	"html/template"
	"io"
	"log"
	"net/http"

	conf "github.com/nearmap/cvmanager/config"
	"github.com/nearmap/cvmanager/events"
	clientset "github.com/nearmap/cvmanager/gok8s/client/clientset/versioned"
	k8s "github.com/nearmap/cvmanager/gok8s/workload"
	"github.com/nearmap/cvmanager/stats"
	"github.com/pkg/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes"
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

// GetAllContainerVersion provides details of current container version of all workload
//  managed by CV managed resource
// supports json and html format specified via typ
func GetAllContainerVersion(w io.Writer, typ string, k8sProvider *k8s.K8sProvider, cs clientset.Interface) error {
	cvs, err := cs.CustomV1().ContainerVersions("").List(metav1.ListOptions{})
	if err != nil {
		return errors.Wrap(err, "Failed to generate template of CV list")
	}

	var cvsList []*k8s.Resource
	for _, cv := range cvs.Items {
		cvs, err := k8sProvider.CVWorkload(&cv)
		if err != nil {
			return errors.Wrap(err, "Failed to generate template of CV list")
		}
		cvsList = append(cvsList, cvs...)
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
func NewCVHandler(cs kubernetes.Interface, customCS clientset.Interface) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		typ := r.URL.Query().Get("format")
		if typ != "json" && typ != "html" {
			typ = "json"
		}

		recorder := events.PodEventRecorder(cs, "")

		k8sProvider := k8s.NewK8sProvider(cs, "", recorder, conf.WithStats(stats.NewFake()))
		err := GetAllContainerVersion(w, typ, k8sProvider, customCS)
		if err != nil {
			log.Printf("failed to get workload list %v", err)
			http.Error(w, http.StatusText(http.StatusInternalServerError), http.StatusInternalServerError)
		}
		// // Allow origin so can easily be used by monitoring
		// w.Header().Set("Access-Control-Allow-Origin", "*")
		// w.Header().Set("X-Frame-Options", "ALLOWALL")
	}
}
