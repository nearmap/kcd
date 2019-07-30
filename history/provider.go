package history

import (
	"fmt"
	"strings"
	"time"

	"github.com/golang/glog"
	"github.com/eric1313/kcd/stats"
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
	Time    time.Time
}

func (r *Record) String() string {
	return fmt.Sprintf("Update occurred at:%s:\nWorkload:%s to version:%s\n", r.Time, r.Name, r.Version)
}

// Provider is an interface to add and fetch kcd release/update history
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
		return "", errors.Wrapf(err, "failed to find kcd history in configmap: %s/%s", namespace, name)
	}
	return cm.Data[key], nil
}

func (p *provider) Add(namespace, name string, record *Record) error {
	cm, err := p.cs.CoreV1().ConfigMaps(namespace).Get(configName(name), metav1.GetOptions{})
	if err != nil {
		if k8serr.IsNotFound(err) {
			_, err = p.cs.CoreV1().ConfigMaps(namespace).Create(newRecordConfig(namespace, name, record))
			if err != nil {
				return errors.Wrapf(err, "failed to create kcd history configmap:%s/%s", namespace, name)
			}
			return nil
		}
		return errors.Wrapf(err, "failed to get kcd history configmap:%s/%s", namespace, name)
	}

	_, err = p.cs.CoreV1().ConfigMaps(namespace).Update(updateRecordConfig(cm, record))
	if err != nil {
		glog.Errorf("failed to update kcd update history in configmap: %s/%s", namespace, name)
		return errors.Wrapf(err, "failed to update kcd update history in configmap: %s/%s", namespace, name)
	}
	return nil
}

func configName(name string) string {
	return fmt.Sprintf("%s.history", name)
}

// newRecordConfig creates a new configmap to capture update history performed by kcd
// specifically syncers
func newRecordConfig(namespace, name string, records ...*Record) *corev1.ConfigMap {
	var strRecords []string
	for _, r := range records {
		strRecords = append(strRecords, r.String())
	}
	msg := strings.Join(strRecords, "\n")
	return &corev1.ConfigMap{
		ObjectMeta: metav1.ObjectMeta{
			Name:      configName(name),
			Namespace: namespace,
			Labels:    labels(),
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

	cm.ObjectMeta.Labels = labels()
	// ConfigMap can only hold 1MB size data
	// see https://github.com/kubernetes/kubernetes/issues/19781
	bytLen := len([]byte(msg))
	if bytLen > sizeLimit {
		msg = msg[:sizeLimit]
	}
	cm.Data[key] = string(msg)
	return cm
}

func labels() map[string]string {
	return map[string]string{
		"OWNED_BY":    "kcd",
		"MODIFIED_AT": time.Now().Format("Mon-2Jan2006-15.04"),
	}
}
