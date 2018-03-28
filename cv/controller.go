package cv

import (
	"fmt"
	"log"
	"reflect"
	"strings"
	"time"

	cv1 "github.com/nearmap/cvmanager/gok8s/apis/custom/v1"
	clientset "github.com/nearmap/cvmanager/gok8s/client/clientset/versioned"
	scheme "github.com/nearmap/cvmanager/gok8s/client/clientset/versioned/scheme"
	informers "github.com/nearmap/cvmanager/gok8s/client/informers/externalversions"
	customlister "github.com/nearmap/cvmanager/gok8s/client/listers/custom/v1"
	"github.com/nearmap/cvmanager/stats"
	"github.com/pkg/errors"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	k8serr "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/util/runtime"
	"k8s.io/apimachinery/pkg/util/wait"
	k8sinformers "k8s.io/client-go/informers"
	"k8s.io/client-go/kubernetes"
	k8sscheme "k8s.io/client-go/kubernetes/scheme"
	typedcorev1 "k8s.io/client-go/kubernetes/typed/core/v1"
	v1lister "k8s.io/client-go/listers/apps/v1"
	"k8s.io/client-go/tools/cache"
	"k8s.io/client-go/tools/record"
	"k8s.io/client-go/util/workqueue"
)

// CVController manages ContainerVersion (CV) kind (a custom resource definition) of resources.
// It ensures that any changes in CV resources are picked up and acted upon.
// The responsibility of CVController is to make sure that the container versions specified by CV resources
// are up-to date. CVController does this by starting a DRSync for the container CV resource requests.
// It is then DRSync service's responsibility to keep the version of a container up to date and perform
// a rolling deployment whenever the container version needs to be updated as per tags of the DR repository.
type CVController struct {
	cluster string
	config  *configKey

	cvImgRepo string

	k8sCS    kubernetes.Interface
	customCS clientset.Interface

	deployLister v1lister.DeploymentLister
	deploySynced cache.InformerSynced

	cvcLister customlister.ContainerVersionLister
	cvcSynced cache.InformerSynced

	queue workqueue.RateLimitingInterface

	recorder record.EventRecorder

	stats stats.Stats
}

type configKey struct {
	name string
	ns   string
}

// NewCVController returns a new container version controller
func NewCVController(configMapKey, cvImgRepo string, k8sCS kubernetes.Interface, customCS clientset.Interface,
	k8sIF k8sinformers.SharedInformerFactory, customIF informers.SharedInformerFactory,
	statsInstance stats.Stats) (*CVController, error) {

	namespace, name, err := cache.SplitMetaNamespaceKey(configMapKey)
	if err != nil {
		return nil, errors.Wrap(err, "Invalid configmap key")
	}

	deploymentInformer := k8sIF.Apps().V1().Deployments()
	cvcInformer := customIF.Custom().V1().ContainerVersions()

	scheme.AddToScheme(k8sscheme.Scheme)

	eventBroadcaster := record.NewBroadcaster()
	eventBroadcaster.StartLogging(log.Printf)
	eventBroadcaster.StartRecordingToSink(&typedcorev1.EventSinkImpl{Interface: k8sCS.CoreV1().Events("")})
	recorder := eventBroadcaster.NewRecorder(k8sscheme.Scheme, corev1.EventSource{Component: "container-version-controller"})

	if statsInstance == nil {
		statsInstance = stats.NewFake()
	}

	cvc := &CVController{
		config: &configKey{
			name: name,
			ns:   namespace,
		},

		cvImgRepo: cvImgRepo,

		k8sCS:    k8sCS,
		customCS: customCS,

		deployLister: deploymentInformer.Lister(),
		deploySynced: deploymentInformer.Informer().HasSynced,

		cvcLister: cvcInformer.Lister(),
		cvcSynced: cvcInformer.Informer().HasSynced,

		queue:    workqueue.NewNamedRateLimitingQueue(workqueue.DefaultControllerRateLimiter(), "ContainerVersions"),
		recorder: recorder,
		stats:    statsInstance,
	}

	log.Printf("Setting up event handlers in container version controller")

	cvcInformer.Informer().AddEventHandler(cache.ResourceEventHandlerFuncs{
		AddFunc: cvc.enqueue,
		UpdateFunc: func(old, new interface{}) {
			if !reflect.DeepEqual(old, new) {
				cvc.enqueue(new)
			}
		},
		DeleteFunc: cvc.dequeueCV,
	})

	// TODO : We need deploymentInformer to monitor DR sycn deployments specs if they were modified
	// outside the controller scope. This need some more work as following snippet causes infinite cycle.
	// Probably checking if state of DR deployment is different than whats specified by CV CRD
	// but we can worry about it later

	// deploymentInformer.Informer().AddEventHandler(cache.ResourceEventHandlerFuncs{
	// 	AddFunc: cvc.handleCVOwnedObj,
	// 	UpdateFunc: func(old, new interface{}) {
	// 		newDepl := new.(*appsv1.Deployment)
	// 		oldDepl := old.(*appsv1.Deployment)
	// 		if newDepl.ResourceVersion == oldDepl.ResourceVersion {
	// 			// Periodic resync will send update events for all known Deployments.
	// 			// Two different versions of the same Deployment will always have different RVs.
	// 			return
	// 		}
	// 		cvc.handleCVOwnedObj(new)
	// 	},
	// 	DeleteFunc: cvc.handleCVOwnedObj,
	// })

	return cvc, nil
}

// Run starts the cv controller so it starts acting as cvcontroller
func (c *CVController) Run(threadiness int, stopCh <-chan struct{}) error {
	defer runtime.HandleCrash()
	defer c.queue.ShutDown()

	log.Printf("Starting Container version controller")

	if !cache.WaitForCacheSync(stopCh, c.deploySynced, c.cvcSynced) {
		return errors.New("Fail to wait for (secondary) cache sync")
	}

	log.Printf("Cache sync completed")

	for i := 0; i < threadiness; i++ {
		go wait.Until(c.runWorker, time.Second, stopCh)
	}
	log.Printf("Started Container version controller")

	<-stopCh
	log.Printf("Shutting down container version controller")
	return nil
}

func (c *CVController) runWorker() {
	log.Printf("Running worker")
	for c.processNextWorkItem() {
	}
}

// processNextWorkItem will read a single work item off the queue and
// attempt to process it, by calling the syncHandler.
func (c *CVController) processNextWorkItem() bool {
	obj, shutdown := c.queue.Get()

	if shutdown {
		return false
	}

	err := func(obj interface{}) error {
		defer c.queue.Done(obj)
		var key string
		var ok bool
		if key, ok = obj.(string); !ok {
			c.queue.Forget(obj)
			runtime.HandleError(fmt.Errorf("expected string in queue but got %#v", obj))
			return nil
		}
		log.Printf("Processing CRD in sync handler: name=%v, crd=%v", key, obj)

		// Run the syncHandler, passing it the namespace/name string of the
		// ContainerVersion resource to be synced.
		if err := c.syncHandler(key); err != nil {
			c.stats.IncCount(fmt.Sprintf("cvc.%s.sync.failure", key))
			return errors.Wrapf(err, "error syncing '%s'", key)
		}
		c.queue.Forget(obj)
		log.Printf("Successfully synced '%s'", key)
		c.stats.IncCount(fmt.Sprintf("cvc.%s.sync.success", key))
		return nil
	}(obj)

	if err != nil {
		runtime.HandleError(err)
		return true
	}

	return true
}

// syncHandler processes the container version resource and creates/updates the deployment
// depending on whether the resource is already present or not
func (c *CVController) syncHandler(key string) error {
	log.Printf("Processing container version resource with name %s", key)

	namespace, name, err := cache.SplitMetaNamespaceKey(key)
	if err != nil {
		runtime.HandleError(fmt.Errorf("invalid resource key: %s", key))
		log.Printf("Failed to get namespacekey from cv spec: %v", err)
		return nil
	}

	cv, err := c.cvcLister.ContainerVersions(namespace).Get(name)
	if err != nil {
		if k8serr.IsNotFound(err) {
			runtime.HandleError(fmt.Errorf("cv '%s' in work queue no longer exists", key))
			return nil
		}
	}

	version, err := c.fetchVersion()
	if err != nil {
		c.recorder.Event(cv, corev1.EventTypeWarning, "FailedCreateDRSync", "Cant find config for DRSync version")
		return errors.Wrap(err, "Failed to find container version")
	}
	if err = c.syncDeployments(namespace, key, version, cv); err != nil {
		return errors.Wrap(err, "Failed to sync deployment")
	}

	log.Printf("In sync handler of CVC for key=%s, namespace=%v, cv=%v, name=%v", key, namespace, cv, name)

	c.recorder.Event(cv, corev1.EventTypeNormal, "Synced", "Sync of CV resource was successful")
	return nil
}

// enqueue takes a CV resource and converts it into a namespace/name
// string which is then put onto the work queue. This method should *not* be
// passed resources of any type other than CV.
func (c *CVController) enqueue(obj interface{}) {
	key, err := cache.DeletionHandlingMetaNamespaceKeyFunc(obj)
	if err != nil {
		runtime.HandleError(fmt.Errorf("error obtaining key for object being enqueue: %s", err.Error()))
		log.Printf("Failed to obtain key for object being enqueue: %v", err)
		return
	}

	log.Printf("Queued cv for processing: name=%s", key)

	c.queue.AddRateLimited(key)
}

// dequeueCV will take any resource implementing metav1.Object and just checks for any error
// situations.. Ideally we dont have to worry about Delete CV but just keeping it
// as more info for now
func (c *CVController) dequeueCV(obj interface{}) {
	var object metav1.Object
	var ok bool
	if object, ok = obj.(metav1.Object); !ok {
		tombstone, ok := obj.(cache.DeletedFinalStateUnknown)
		if !ok {
			runtime.HandleError(fmt.Errorf("error decoding object, invalid type"))
			return
		}
		object, ok = tombstone.Obj.(metav1.Object)
		if !ok {
			runtime.HandleError(fmt.Errorf("error decoding object tombstone, invalid type"))
			return
		}
		log.Printf("Recovered deleted object '%s' from tombstone", object.GetName())
	}
	// All resources owned by CV will automatically be deleted so nothing needs to be done
	log.Printf("Successfully dequeued object cv'%s/%s'", object.GetNamespace(), object.GetName())
}

// handleCVOwnedObj will take any resource implementing metav1.Object and attempt
// to find the ContainerVersion resource that 'owns' it. It does this by looking at the
// objects metadata.ownerReferences field for an appropriate OwnerReference.
// It then enqueues that ContainerVersion resource to be processed. If the object does not
// have an appropriate OwnerReference, it will simply be skipped.
func (c *CVController) handleCVOwnedObj(obj interface{}) {
	var object metav1.Object
	var ok bool
	if object, ok = obj.(metav1.Object); !ok {
		tombstone, ok := obj.(cache.DeletedFinalStateUnknown)
		if !ok {
			runtime.HandleError(fmt.Errorf("error decoding object, invalid type"))
			return
		}
		object, ok = tombstone.Obj.(metav1.Object)
		if !ok {
			runtime.HandleError(fmt.Errorf("error decoding object tombstone, invalid type"))
			return
		}
		log.Printf("Recovered deleted object '%s' from tombstone", object.GetName())
	}

	if ownerRef := metav1.GetControllerOf(object); ownerRef != nil {
		key := fmt.Sprintf("%s/%s", object.GetNamespace(), object.GetName())
		log.Printf("Container version controller owned object info: key=%s, owner=%v, obj=%v",
			key, metav1.GetControllerOf(object), object)

		if ownerRef.Kind != "ContainerVersion" {
			return
		}

		cv, err := c.cvcLister.ContainerVersions(object.GetNamespace()).Get(ownerRef.Name)
		if err != nil {
			log.Printf("ignoring orphaned object '%s' of CV'%s'", object.GetSelfLink(), ownerRef.Name)
			return
		}
		c.enqueue(cv)
		return
	}
}

// syncDeployments sync the deployment referenced by CV resource - creates if absent and updates if required
// The synce deployments are automatically updated when controller is updated so the syncers do not need CV
// resource or auto update mechanism
func (c *CVController) syncDeployments(namespace, key, version string, cv *cv1.ContainerVersion) error {
	_, err := c.deployLister.Deployments(namespace).Get(syncDeployment(cv.Spec.Deployment.Name))
	if err != nil {
		if k8serr.IsNotFound(err) {
			_, err = c.k8sCS.AppsV1().Deployments(namespace).Create(c.newDRSyncDeployment(cv, version))
			if err != nil {
				c.recorder.Event(cv, corev1.EventTypeWarning, "FailedCreateDRSync", "Failed to create DR Sync deployment")
				return errors.Wrapf(err, "Failed to create DR Sync deployment %s", key)
			}
			return nil
		}
		return errors.Wrapf(err, "Failed to find DR Sync deployment %s", key)
	}

	_, err = c.k8sCS.AppsV1().Deployments(namespace).Update(c.newDRSyncDeployment(cv, version))
	if err != nil {
		c.recorder.Event(cv, corev1.EventTypeWarning, "FailedUpdateDRSync", "Failed to update DR Sync deployment")
		return errors.Wrapf(err, "Failed to update DR Sync deployment %s", key)
	}
	return nil
}

// newDRSyncDeployment creates a new Deployment for a ContainerVersion resource. It also sets
// the appropriate OwnerReferences on the resource so we can discover
// the ContainerVersion resource that 'owns' it.
// TODO We need to improve on auto-upgrading DRsync deployments ..
func (c *CVController) newDRSyncDeployment(cv *cv1.ContainerVersion, version string) *appsv1.Deployment {
	nr := int32(1)
	var configKey, provider string
	if cv.Spec.Config != nil {
		configKey = fmt.Sprintf("%s/%s", cv.Spec.Config.Name, cv.Spec.Config.Key)
	}
	dName := syncDeployment(cv.Spec.Deployment.Name)
	if strings.Contains(cv.Spec.ImageRepo, "amazonaws.com") {
		provider = "ecr"
	} else {
		provider = "dockerhub"
	}

	labels := map[string]string{
		"app":        "dr-syncer",
		"controller": cv.Name,
	}
	return &appsv1.Deployment{
		ObjectMeta: metav1.ObjectMeta{
			Name:      dName,
			Namespace: cv.Namespace,
			OwnerReferences: []metav1.OwnerReference{
				*metav1.NewControllerRef(cv, schema.GroupVersionKind{
					Group:   cv1.SchemeGroupVersion.Group,
					Version: cv1.SchemeGroupVersion.Version,
					Kind:    "ContainerVersion",
				}),
			},
		},
		Spec: appsv1.DeploymentSpec{
			Replicas: &nr,
			Strategy: appsv1.DeploymentStrategy{
				Type: appsv1.RollingUpdateDeploymentStrategyType,
			},
			Selector: &metav1.LabelSelector{
				MatchLabels: labels,
			},
			Template: corev1.PodTemplateSpec{
				ObjectMeta: metav1.ObjectMeta{
					Labels: labels,
				},
				Spec: corev1.PodSpec{
					Containers: []corev1.Container{
						{
							Name:  fmt.Sprintf("%s-container", dName),
							Image: fmt.Sprintf("%s:%s", c.cvImgRepo, version),
							Args: []string{
								"dr",
								"sync",
								fmt.Sprintf("--tag=%s", cv.Spec.Tag),
								fmt.Sprintf("--repo=%s", cv.Spec.ImageRepo),
								fmt.Sprintf("--deployment=%s", cv.Spec.Deployment.Name),
								fmt.Sprintf("--container=%s", cv.Spec.Deployment.Container),
								fmt.Sprintf("--namespace=%s", cv.Namespace),
								fmt.Sprintf("--sync=%d", cv.Spec.CheckFrequency),
								fmt.Sprintf("--configKey=%s", configKey),
								fmt.Sprintf("--provider=%s", provider),
							},
							Env: []corev1.EnvVar{{
								Name: "INSTANCENAME",
								ValueFrom: &corev1.EnvVarSource{
									FieldRef: &corev1.ObjectFieldSelector{
										FieldPath: "metadata.name",
									},
								},
							},
								{
									Name: "STATS_HOST",
									ValueFrom: &corev1.EnvVarSource{
										FieldRef: &corev1.ObjectFieldSelector{
											FieldPath: "status.hostIP",
										},
									},
								},
							},
						},
					},
				},
			},
		},
	}
}

func syncDeployment(targetDeployment string) string {
	return fmt.Sprintf("drsync-%s", targetDeployment)
}

// fetchVersion gets container version from config map as specified in configMapKey
func (c *CVController) fetchVersion() (string, error) {
	cm, err := c.k8sCS.CoreV1().ConfigMaps(c.config.ns).Get(c.config.name, metav1.GetOptions{})
	if err != nil {
		return "", errors.Wrap(err, "Failed to get config for version for sync service")
	}
	version := cm.Data["version"]
	if version == "" {
		return "", errors.Wrap(err, "Missing config map cvmanager in kube-system")
	}
	return version, nil
}
