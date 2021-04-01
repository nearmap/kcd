package events

import (
	"context"
	"encoding/json"
	"github.com/golang/glog"
	"github.com/wish/kcd/registry/ecr"
	"github.com/wish/kcd/stats"
	"k8s.io/api/admission/v1beta1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"reflect"
	"regexp"
	"strings"
)


// objectWithMeta allows us to unmarshal just the ObjectMeta of a k8s object
type objectWithMeta struct {
	metav1.ObjectMeta `json:"metadata,omitempty" protobuf:"bytes,1,opt,name=metadata"`
}

type containaerData struct {
	name string `json:"name,omitempty"`
	image string `json:"name,omitempty"`
}

type Record map[string]interface{}

type patchOperation struct {
	Op    string      `json:"op"`
	Path  string      `json:"path"`
	Value interface{} `json:"value,omitempty"`
}

var versionRegex, _ = regexp.Compile(`[0-9a-f]{5,40}`)

// Get the value addressed by `nameParts`
func (r Record) Get(nameParts []string, cName string) (string, bool) {
	// If no key is given, return nothing
	if nameParts == nil || len(nameParts) <= 0 {
		return nil, false
	}
	val, ok := r[nameParts[0]]
	if !ok {
		return nil, ok
	}

	for _, namePart := range nameParts[1:] {
		typedVal, ok := val.(map[string]interface{})
		if !ok {
			return nil, ok
		}
		val, ok = typedVal[namePart]
		if !ok {
			return nil, ok
		}
	}

	containers := val.([]interface{})

	for _, container := range containers {
		d := container.(containaerData)
		if d.name == cName {
			return d.image, true
		}
	}

	return "", false
}


func Mutate(req *v1beta1.AdmissionRequest, stats stats.Stats) *v1beta1.AdmissionResponse {
	var newManifest objectWithMeta
	if err := json.Unmarshal(req.Object.Raw, &newManifest); err != nil {
		glog.Errorf("Could not unmarshal raw object: %v", err)
		return &v1beta1.AdmissionResponse{
			Result: &metav1.Status{
				Message: err.Error(),
			},
		}
	}

	glog.Infof("AdmissionReview for Kind=%v, Namespace=%v Name=%v (%v) UID=%v patchOperation=%v UserInfo=%v",
		req.Kind, req.Namespace, req.Name, newManifest.Name, req.UID, req.Operation, req.UserInfo)

	var currentMap map[string]interface{}

	if req.OldObject.Raw != nil {
		if err := json.Unmarshal(req.OldObject.Raw, &currentMap); err != nil {
			return &v1beta1.AdmissionResponse{
				Result: &metav1.Status{
					Message: err.Error(),
				},
			}
		}
	}

	var newMap map[string]interface{}
	if err := json.Unmarshal(req.Object.Raw, &newMap); err != nil {
		return &v1beta1.AdmissionResponse{
			Result: &metav1.Status{
				Message: err.Error(),
			},
		}
	}

	patches := patchForContainer("merchan-backend", Record(currentMap), Record(newMap), stats)

	if len(patches) == 0 {
		return &v1beta1.AdmissionResponse{
			Allowed: true,
		}
	}

	patchBytes, err := json.Marshal(patches)
	if err != nil {
		return &v1beta1.AdmissionResponse{
			Result: &metav1.Status{
				Message: err.Error(),
			},
		}
	}

	glog.Infof("AdmissionResponse: patch=%v\n", string(patchBytes))
	return &v1beta1.AdmissionResponse{
		Allowed: true,
		Patch:   patchBytes,
		PatchType: func() *v1beta1.PatchType {
			pt := v1beta1.PatchTypeJSONPatch
			return &pt
		}(),
	}

}

// patchForPath returns any patches required to get the value at `path` to match
// in replacement if already set in current
func patchForContainer(cName string, current, replacement Record, stats stats.Stats) ([]patchOperation, bool) {

	path := "/spec/template/spec/containers"

	pathParts := strings.Split(strings.Trim(path, "/"), "/")

	// If the current one doesn't have the value, we're okay to let this pass
	imageRepo, ok := current.Get(pathParts, cName)
	if !ok {
		return nil
	}

	patchOp := patchOperation{
		Path:  path,
		Value: imageRepo,
	}

	patches := []patchOperation{patchOp}

	imageData := strings.Split(imageRepo, ":")
	curVer := imageData[1]
	ctx := c

	if versionRegex.MatchString(curVer) {
		glog.Infof("Already SHA tag, no need to patch for container %v at version: %v",  cName, curVer)
		return patches, false
	} else {
		p, e := ecr.NewECR(imageRepo, "[0-9a-f]{5,40}", stats)
		if e != nil {
			glog.Errorf("Unable to create ECR for iamge repo %v at version: %v", imageRepo, curVer)
			return patches, false
		}
		if registry, e := p.RegistryFor(imageRepo); e != nil {

		} else {
			registry.Versions()
		}
	}




	newV, ok := replacement.Get(pathParts)

	if ok {
		// If the replacement has the value and it matches, we need no patch
		if reflect.DeepEqual(tagVersion, newV) {
			return nil
		}

		// if the replacement has the path but they don't match, it needs to be replaced
		glog.Infof("Replacing path=%s old=%v new=%v", path, currentV, newV)
		patchOp.Op = "replace"
	} else {
		// if the replacement doesn't  have the path, we need to add it
		glog.Infof("Setting path=%s value=%v", path, currentV)
		patchOp.Op = "add"
	}

	return []patchOperation{patchOp}
}


