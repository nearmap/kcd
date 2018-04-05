package cv

import (
	"encoding/json"
	"html/template"
	"io"
	"net/http"
	"strings"

	clientset "github.com/nearmap/cvmanager/gok8s/client/clientset/versioned"
	"github.com/pkg/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes"
)

// Workloads maintains a high level status of deployments managed by
// CV resources including version of current deploy and number of available pods
// from this deployment/relicaset
type Workloads struct {
	Namespace     string
	Name          string
	Container     string
	Version       string
	AvailablePods int32
}

func getCVs(cs kubernetes.Interface, customCS clientset.Interface) ([]*Workloads, error) {

	cvs, err := customCS.CustomV1().ContainerVersions("").List(metav1.ListOptions{})
	if err != nil {
		return nil, errors.Wrap(err, "Failed to fetch CV resources")
	}
	var cvsList []*Workloads
	for _, cv := range cvs.Items {
		dd, err := cs.AppsV1().Deployments(cv.Namespace).Get(cv.Spec.Deployment.Name, metav1.GetOptions{})
		if err != nil {
			return nil, errors.Wrap(err, "Failed to fetch deployment")
		}

		for _, c := range dd.Spec.Template.Spec.Containers {
			if cv.Spec.Deployment.Container == c.Name {
				cvsList = append(cvsList, &Workloads{
					Namespace:     cv.Namespace,
					Name:          dd.Name,
					Container:     c.Name,
					Version:       strings.SplitAfterN(c.Image, ":", 2)[1],
					AvailablePods: dd.Status.AvailableReplicas,
				})
			}
		}
	}

	return cvsList, nil
}

func genCVHTML(w io.Writer, cvs []*Workloads) error {
	t := template.Must(template.New("cvList").Parse(cvListHTML))
	err := t.Execute(w, cvs)
	if err != nil {
		return errors.Wrap(err, "Failed to generate template of CV list")
	}
	return nil
}

func genCV(w io.Writer, cvs []*Workloads) error {
	bytes, err := json.Marshal(cvs)
	if err != nil {
		return errors.Wrap(err, "Failed to convert cv list to json")
	}

	w.Write(bytes)
	return nil
}

// ExecuteWorkloadsList generates HTML tabular text listing all status of all CV managed resource
// as represented by Workloads
func ExecuteWorkloadsList(w io.Writer, typ string, cs kubernetes.Interface, customCS clientset.Interface) error {
	cvs, err := getCVs(cs, customCS)
	if err != nil {
		return errors.Wrap(err, "Failed to generate template of CV list")
	}

	switch typ {
	case "html":
		return genCVHTML(w, cvs)
	case "json":
		return genCV(w, cvs)
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
		err := ExecuteWorkloadsList(w, typ, cs, customCS)
		if err != nil {
			http.Error(w, http.StatusText(http.StatusInternalServerError), http.StatusInternalServerError)
		}
	}
}
