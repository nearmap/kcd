package events

import (
	"fmt"
	"k8s.io/api/admission/v1beta1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"testing"
)

type admissionResponse struct {
	Allowed bool

	StatusMessage string
	Patch         string
}

func (a *admissionResponse) Validate(ar *v1beta1.AdmissionResponse) error {
	if a.Allowed != ar.Allowed {
		return fmt.Errorf("Mismatch in allowed expected=%v actual=%v", a.Allowed, ar.Allowed)
	}

	if ar.Result == nil {
		if a.StatusMessage != "" {
			return fmt.Errorf("No status message when expecting: %s", a.StatusMessage)
		}
	} else {
		if a.StatusMessage != ar.Result.Message {
			return fmt.Errorf("Mismatch in StatusMessage expected=%v actual=%v", a.StatusMessage, ar.Result.Message)
		}
	}

	if ar.Patch == nil {
		if a.Patch != "" {
			return fmt.Errorf("No patch when expecting: %s", a.Patch)
		}
	} else {
		if a.Patch != string(ar.Patch) {
			return fmt.Errorf("Mismatch in Patch expected=%s actual=%s", a.Patch, ar.Patch)
		}
	}

	return nil
}

func TestMutate(t *testing.T) {
	tests := []struct {
		in  *v1beta1.AdmissionRequest
		out *admissionResponse
	}{
		//// malformed input
		//{
		//	in: &v1beta1.AdmissionRequest{
		//		Object: runtime.RawExtension{
		//			Raw: []byte(``),
		//		},
		//	},
		//	out: &admissionResponse{
		//		Allowed:       false,
		//		StatusMessage: "unexpected end of JSON input",
		//	},
		//},
		//
		//// object with no labels -- which should be allowed
		//{
		//	in: &v1beta1.AdmissionRequest{
		//		Object: runtime.RawExtension{
		//			Raw: []byte(`{}`),
		//		},
		//	},
		//	out: &admissionResponse{
		//		Allowed: true,
		//	},
		//},

		// object with labels enabling this hook with a path defined that doesn't exist in new
		{
			in: &v1beta1.AdmissionRequest{
				Kind: metav1.GroupVersionKind{
					Group:   "apps",
					Version: "v1",
					Kind:    "Deployment",
				},
				Object: runtime.RawExtension{
					Raw: []byte(`{
					    "metadata": {
					        "annotations": {"kcd-version-patcher.wish.com/container": "hello-service"},
					        "labels": {"kcd-version-patcher.wish.com/enabled": "true"},
					        "namespace": "hello-service",
					        "name": "hello-service"
				        },
				        "spec": {
							"template": {
								"spec": {
									"containers": [
										{
											"name": "hello-service",
											"image": "951896542015.dkr.ecr.us-west-1.amazonaws.com/contextlogic/hello-service:93ebd365"
										}
									]
								}
							}
						}
				    }`),
				},
				OldObject: runtime.RawExtension{
					Raw: []byte(`{
					    "metadata": {
					        "annotations": {"kcd-version-patcher.wish.com/container": "hello-service"},
					        "labels": {"kcd-version-patcher.wish.com/enabled": "true"},
					        "namespace": "hello-service",
					        "name": "hello-service"
				        },
				        "spec": {
							"template": {
								"spec": {
									"containers": [
										{
											"name": "hello-service",
											"image": "951896542015.dkr.ecr.us-west-1.amazonaws.com/contextlogic/hello-service:93ebd365"
										}
									]
								}
							}
						}
				    }`),
				},
			},
			out: &admissionResponse{
				Allowed: true,
				Patch:   `[{"op":"replace","path":"/spec/template/spec/containers/0/image","value":"951896542015.dkr.ecr.us-west-1.amazonaws.com/contextlogic/hello-service:93ebd365"}]`,
			},
		},
	}

	for _, test := range tests {
		out := Mutate(test.in, nil)
		if err := test.out.Validate(out); err != nil {
			t.Fatalf("Error: %v\n%v", err, out)
		}
	}
}