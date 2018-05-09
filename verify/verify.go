package verify

import (
	"github.com/nearmap/cvmanager/events"
	cv1 "github.com/nearmap/cvmanager/gok8s/apis/custom/v1"
	"github.com/nearmap/cvmanager/stats"
	"github.com/pkg/errors"
	"k8s.io/client-go/kubernetes"
)

var (
	Failed = errors.New("Verification failed")
)

type Verifier interface {
	Verify() error
}

func NewVerifier(cs kubernetes.Interface, recorder events.Recorder, stats stats.Stats, ns string,
	spec *cv1.VerifySpec) (Verifier, error) {
	var verifier Verifier
	switch spec.Kind {
	case KindImage:
		verifier = NewImageVerifier(cs, recorder, stats, ns, spec)
	default:
		return nil, errors.Errorf("unknown verify type: %v", spec.Kind)
	}
	return verifier, nil
}
