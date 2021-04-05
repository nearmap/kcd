package events

import (
	"context"
	"encoding/json"
	"fmt"
	"github.com/golang/glog"
	"github.com/mitchellh/mapstructure"
	"github.com/wish/kcd/registry/ecr"
	"github.com/wish/kcd/stats"
	"k8s.io/api/admission/v1beta1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"regexp"
	"strconv"
	"strings"
)


const (
	EnabledLabel = "kcd-version-patcher.wish.com/enabled"

	PathsAnnotationKey = "kcd-version-patcher.wish.com/container"

	ContainerPatchPath = "/spec/template/spec/containers"

	VersionRegex = "[0-9a-f]{5,40}"
)

// objectWithMeta allows us to unmarshal just the ObjectMeta of a k8s object
type objectWithMeta struct {
	metav1.ObjectMeta `json:"metadata,omitempty" protobuf:"bytes,1,opt,name=metadata"`
}

type containaerData struct {
	Name string `mapstructure:"name"`
	Image string `mapstructure:"image"`
}

type Record map[string]interface{}

type patchOperation struct {
	Op    string      `json:"op"`
	Path  string      `json:"path"`
	Value interface{} `json:"value,omitempty"`
}

var versionRegex, _ = regexp.Compile(`[0-9a-f]{5,40}`)

// Get the container image string alue addressed by `nameParts`
func (r Record) Get(nameParts []string, cName string) (string, string, bool) {
	// If no key is given, return nothing
	if nameParts == nil || len(nameParts) <= 0 {
		return "", "-1", false
	}
	val, ok := r[nameParts[0]]
	if !ok {
		return "", "-1", ok
	}

	for _, pathName := range nameParts[1:] {
		typedVal, ok := val.(map[string]interface{})
		if !ok {
			return "", "-1", ok
		}
		val, ok = typedVal[pathName]
		if !ok {
			return "", "-1", ok
		}
	}

	containers := val.([]interface{})
	matched := false
	for idx, container := range containers {
		var cd containaerData
		mapstructure.Decode(container, &cd)
		if cd.Name == cName {
			matched = true
			glog.V(4).Infof("Found specified container name, start patching: %s", cName)
			return cd.Image, strconv.Itoa(idx), true
		}
	}

	if !matched {
		glog.V(4).Infof("Could not find specified container name: %s", cName)
	}

	return "", "-1", false
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

	glog.V(4).Infof("AdmissionReview for Kind=%v, Namespace=%v Name=%v (%v) UID=%v patchOperation=%v UserInfo=%v",
		req.Kind, req.Namespace, req.Name, newManifest.Name, req.UID, req.Operation, req.UserInfo)

	// if no enabled labeld is there, we skip the patching and passing the request.
	v, ok := newManifest.Labels[EnabledLabel]
	if !ok {
		glog.V(4).Info("No label defined kcd-version-patcher.wish.com/enabled")
		return &v1beta1.AdmissionResponse{
			Allowed: true,
			Result: &metav1.Status{
				Message: "Patching does not have defined boolean value enable: true or false",
			},
		}
	}
	// if enable label is not TRUE or not boolean, pass the checking
	if b, err := strconv.ParseBool(v); err != nil {
		glog.V(4).Infof("Label kcd-version-patcher.wish.com/enabled is not boolean: %v", v)
		return &v1beta1.AdmissionResponse{
			Allowed: true,
			Result: &metav1.Status{
				Message: "Patching enabled is not boolean value",
			},
		}
	} else if !b {
		glog.V(4).Infof("Label kcd-version-patcher.wish.com/enabled is not true: %v", v)
		return &v1beta1.AdmissionResponse{
			Allowed: true,
			Result: &metav1.Status{
				Message: "Patching is disabled",
			},
		}
	}

	pathAnnotations := newManifest.GetAnnotations()
	containerName, ok := pathAnnotations[PathsAnnotationKey]
	if !ok {
		return &v1beta1.AdmissionResponse{
			Allowed: true,
			Result: &metav1.Status{
				Message: "Patching does not have defined path",
			},
		}
	}

	// In case there is space inside
	containerName = strings.TrimSpace(containerName)

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

	patches, ok := patchForContainer(containerName, Record(currentMap), Record(newMap), stats)

	// if we tried to patch the container name specified in path, but not successful.
	if !ok {
		glog.Errorf("Patching service container %v is failed", Record(currentMap))
		return &v1beta1.AdmissionResponse{
			Allowed: true,
			Result: &metav1.Status{
				Message: "Patching is not successful",
			},
		}
	}

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

	glog.V(4).Infof("AdmissionResponse: patch=%v\n", string(patchBytes))
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

	// We use constant path to retrieve the specified container
	pathParts := strings.Split(strings.Trim(ContainerPatchPath, "/"), "/")

	// If the current one doesn't have the value, we're okay to let this pass
	imageRepo, _, ok := current.Get(pathParts, cName)
	if !ok {
		glog.V(4).Infof("Error getting current image repo container name: %v", cName)
		return nil, false
	}

	// Retrieve current tag from k8s config
	imageData := strings.Split(imageRepo, ":")
	curTag := imageData[1]
	glog.V(4).Infof("Current tag: %v for running container: %v", curTag, cName)

	//We retrieve the image repo and index from replacement map
	imageRepoFlux, idxFlux, ok := replacement.Get(pathParts, cName)
	if !ok {
		glog.Errorf("Error getting new image repo container name applied by flux: %v", cName)
		return nil, false
	}
	// Retrieve new tag applied by flux
	imageDataFlux := strings.Split(imageRepoFlux, ":")
	fluxTag := imageDataFlux[1]
	// If current tag is already a SHA, no need to patch
	if versionRegex.MatchString(fluxTag) {
		glog.V(4).Infof("Already SHA tag of flux applied, no need to patch for container %v at version: %v", cName, curTag)
		return nil, true
	}

	fmt.Printf("Flux tag: %s", fluxTag)

	pathToPatch := strings.Join([]string{ContainerPatchPath, idxFlux, "image"}, "/")

	patchOp := patchOperation{
		Path:  pathToPatch,
	}

	p, e := ecr.NewECR(imageRepoFlux, VersionRegex, stats)
	if e != nil {
		glog.Errorf("Unable to create ECR for iamge repo %v at version: %v", imageRepo, curTag)
		return nil, false
	}
	if registry, e := p.RegistryFor(imageRepoFlux); e != nil {
		glog.Errorf("Failed to build registry for image repo: %v", imageRepo)
		return nil, false
	} else {
		versions, err := registry.Versions(context.Background(), fluxTag)
		if err != nil {
			glog.Errorf("Syncer failed to get version from registry using tag=%s", fluxTag)
		}
		version := versions[0]
		glog.Infof("Got registry versions for container=%s, tag=%s, rolloutVersion=%s", cName, fluxTag, version)

		patchOp.Value = imageDataFlux[0] + ":" + version
		glog.Infof("Replacing path=%v old tag=%v to patched version=%v", pathToPatch, fluxTag, version)
		fmt.Printf("Fectched version: %s", version)
		patchOp.Op = "replace"
		patches := []patchOperation{patchOp}
		return patches, true
	}
}


