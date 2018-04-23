package k8s

/*
const daemonSet = "DaemonSet"

func (k *K8sProvider) syncDaemonSets(cv *cv1.ContainerVersion, version string, listOpts metav1.ListOptions) error {
	ds, err := k.cs.AppsV1().DaemonSets(k.namespace).List(listOpts)
	if err != nil {
		k.Recorder.Event(events.Warning, "CRSyncFailed", "Failed to get dependent daemonSet")
		return errors.Wrap(err, "failed to read daemonSet ")
	}
	for _, d := range ds.Items {
		if ci, err := k.checkPodSpec(d.Spec.Template, d.Name, version, cv); err != nil {
			if err == errs.ErrVersionMismatch {
				return k.deployer.Deploy(d.Spec.Template, d.Name, ci, cv, func(i int) error {
					_, err := k.cs.AppsV1().DaemonSets(k.namespace).Patch(d.ObjectMeta.Name, types.StrategicMergePatchType,
						[]byte(fmt.Sprintf(podTemplateSpec, d.Spec.Template.Spec.Containers[i].Name, cv.Spec.ImageRepo, version)))
					return err

				}, func() string { return daemonSet })
			} else {
				k.raiseSyncPodErrEvents(err, daemonSet, d.Name, cv.Spec.Tag, version)
			}
		}
	}
	return nil
}

func (k *K8sProvider) cvDaemonSets(cv *cv1.ContainerVersion, listOpts metav1.ListOptions) ([]*Resource, error) {
	ds, err := k.cs.AppsV1().DaemonSets(cv.Namespace).List(listOpts)
	if err != nil {
		return nil, errors.Wrap(err, "Failed to fetch daemonset")
	}
	var cvsList []*Resource
	for _, dd := range ds.Items {
		for _, c := range dd.Spec.Template.Spec.Containers {
			if cv.Spec.Container == c.Name {
				cvsList = append(cvsList, &Resource{
					Namespace:     cv.Namespace,
					Name:          dd.Name,
					Type:          daemonSet,
					Container:     c.Name,
					Version:       strings.SplitAfterN(c.Image, ":", 2)[1],
					AvailablePods: dd.Status.NumberAvailable,
					CV:            cv.Name,
					Tag:           cv.Spec.Tag,
				})
			}
		}
	}

	return cvsList, nil
}
*/
