package cv

import (
	"html/template"
	"io"
	"net/http"
	"strings"

	clientset "github.com/nearmap/cvmanager/gok8s/client/clientset/versioned"
	"github.com/pkg/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes"
)

// const cvListHTML = `
// <div class="timeline">
// {{range .}}
// <div class="note">
//     <p class="noteHeading">{{.Namespace}}</p>
//     <hr>
//     <p class="noteContent">{{.Deployment}}</p>
//     <hr>
//     <p class="noteContent">{{.Container}}</p>
//     <hr>
//     <p class="noteContent">{{.Version}}</p>
//     <hr>
//     <p class="noteContent">{{.AvailablePods}}</p>
//     </ul>
//     </span>
// </div>
// {{end}}
// `

type CVStatus struct {
	Namespace     string
	Deployment    string
	Container     string
	Version       string
	AvailablePods int
}

func getCVs(cs kubernetes.Interface, customCS clientset.Interface) ([]*CVStatus, error) {

	cvs, err := customCS.CustomV1().ContainerVersions("").List(metav1.ListOptions{})
	if err != nil {
		return nil, errors.Wrap(err, "Failed to fetch CV resources")
	}
	var cvsList []*CVStatus
	for _, cv := range cvs.Items {
		dd, err := cs.AppsV1().Deployments(cv.Namespace).Get(cv.Spec.Deployment.Name, metav1.GetOptions{})
		if err != nil {
			return nil, errors.Wrap(err, "Failed to fetch deployment")
		}

		for _, c := range dd.Spec.Template.Spec.Containers {
			if cv.Spec.Deployment.Container == c.Name {
				cvsList = append(cvsList, &CVStatus{
					Namespace:     cv.Namespace,
					Deployment:    dd.Name,
					Container:     c.Name,
					Version:       strings.SplitAfterN(c.Image, ":", 2)[1],
					AvailablePods: dd.Status.AvailableReplicas,
				})
			}
		}
	}

	return cvsList, nil
}

func genCVHTML(w io.Writer, cvs []*CVStatus) error {
	t := template.Must(template.New("cvList").Parse(cvListHTML))
	err := t.Execute(w, cvs)
	if err != nil {
		return errors.Wrap(err, "Failed to generate template of CV list")
	}
	return nil
}

func ExecuteCVStatusList(w io.Writer, cs kubernetes.Interface, customCS clientset.Interface) error {
	cvs, err := getCVs(cs, customCS)
	if err != nil {
		return errors.Wrap(err, "Failed to generate template of CV list")
	}
	return genCVHTML(w, cvs)
}

func NewCVHandler(cs kubernetes.Interface, customCS clientset.Interface) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		err := ExecuteCVStatusList(w, cs, customCS)
		if err != nil {
			http.Error(w, http.StatusText(http.StatusInternalServerError), http.StatusInternalServerError)
		}
	}
}