package history

import (
	"fmt"
	"log"
	"strings"
	"time"

	"github.com/nearmap/cvmanager/stats"
	"github.com/pkg/errors"
	corev1 "k8s.io/api/core/v1"
	k8serr "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes"
)

const (
	key       = "Info"
	sizeLimit = 1000000
)

// Record contains details of a release
type Record struct {
	Type    string
	Name    string
	Version string
	time    time.Time
}

func (r *Record) String() string {
	return fmt.Sprintf("Update occurred at:%s:\nWorkload:%s to version:%s\n", r.time, r.Name, r.Version)
}

// Provider is an interface to add and fetch cv release/update history
type Provider interface {
	// History returns the historical record in CV update history
	History(namespace, name string) (string, error)
	// Add adds the history record in CV update history
	Add(namespace, name string, record *Record) error
}

type provider struct {
	cs    kubernetes.Interface
	stats stats.Stats
}

func NewProvider(cs kubernetes.Interface, stats stats.Stats) *provider {
	return &provider{
		cs:    cs,
		stats: stats,
	}
}

func (p *provider) History(namespace, name string) (string, error) {
	cm, err := p.cs.CoreV1().ConfigMaps(namespace).Get(configName(name), metav1.GetOptions{})
	if err != nil {
		if k8serr.IsNotFound(err) {
			return "", nil
		}
		return "", errors.Wrapf(err, "failed to find cv history in configmap: %s/%s", namespace, name)
	}
	return cm.Data[key], nil
}

func (p *provider) Add(namespace, name string, record *Record) error {
	cm, err := p.cs.CoreV1().ConfigMaps(namespace).Get(configName(name), metav1.GetOptions{})
	if err != nil {
		if k8serr.IsNotFound(err) {
			_, err = p.cs.CoreV1().ConfigMaps(namespace).Create(newRecordConfig(namespace, name, record))
			if err != nil {
				return errors.Wrapf(err, "failed to create cv history configmap:%s/%s", namespace, name)
			}
			return nil
		}
		return errors.Wrapf(err, "failed to get cv history configmap:%s/%s", namespace, name)
	}

	_, err = p.cs.CoreV1().ConfigMaps(namespace).Update(updateRecordConfig(cm, record))
	if err != nil {
		log.Printf("failed to update cv update history in configmap: %s/%s", namespace, name)
		return errors.Wrapf(err, "failed to update cv update history in configmap: %s/%s", namespace, name)
	}
	return nil
}

func configName(name string) string {
	return fmt.Sprintf("%s_history", name)
}

// newRecordConfig creates a new configmap to capture update history performed by cvmanager
// specifically syncers
func newRecordConfig(namespace, name string, records ...*Record) *corev1.ConfigMap {
	var strRecords []string
	for _, r := range records {
		strRecords = append(strRecords, r.String())
	}
	msg := strings.Join(strRecords, "\n")
	return &corev1.ConfigMap{
		ObjectMeta: metav1.ObjectMeta{
			Name:      name,
			Namespace: namespace,
		},
		Data: map[string]string{
			key: msg,
		},
	}
}

// updateRecordConfig update history configmap with new update records
func updateRecordConfig(cm *corev1.ConfigMap, records ...*Record) *corev1.ConfigMap {
	var strRecords []string
	for _, r := range records {
		strRecords = append(strRecords, r.String())
	}
	msg := strings.Join(strRecords, "\n")
	msg = fmt.Sprintf("%s\n%s", msg, cm.Data[key])

	// ConfigMap can only hold 1MB size data
	// see https://github.com/kubernetes/kubernetes/issues/19781
	cm.Data[key] = string(msg[:sizeLimit])
	return cm
}
