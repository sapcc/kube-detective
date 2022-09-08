package detective

import (
	"context"
	"fmt"
	"strconv"
	"time"

	core "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	meta "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/labels"
	"k8s.io/apimachinery/pkg/util/intstr"
	"k8s.io/apimachinery/pkg/util/wait"
)

func (d *Detective) createNamespace() error {
	//glog.V(2).Infof("Creating Namespace")
	d.log.Info("Creating Namespace")
	spec := &core.Namespace{
		ObjectMeta: meta.ObjectMeta{
			GenerateName: "detective-",
		},
		Status: core.NamespaceStatus{},
	}

	ns, err := d.client.CoreV1().Namespaces().Create(d.tomb.Context(nil), spec, meta.CreateOptions{})
	if err != nil {
		return err
	}

	d.namespace = ns
	//glog.V(3).Infof("  created %v", ns.Name)
	d.log.Info("created", "namespace", ns.Name)

	return nil
}

func (d *Detective) deleteNamespace() error {
	//glog.V(2).Infof("Deleting Namespace")
	d.log.Info("deleting namespace")
	err := d.client.CoreV1().Namespaces().Delete(context.Background(), d.namespace.Name, meta.DeleteOptions{})
	if err != nil {
		return err
	}
	//glog.V(3).Infof("  deleted %v", d.namespace.Name)
	d.log.Info("deleted namespace", "namespace", d.namespace.Name)
	return nil
}

func (d *Detective) waitForServiceAccountInNamespace() error {
	//glog.V(2).Info("Waiting for Service Account")
	d.log.Info("waiting for service account")

	return wait.PollImmediateUntil(1*time.Second, func() (done bool, err error) {
		_, err = d.client.CoreV1().ServiceAccounts(d.namespace.Name).Get(d.tomb.Context(nil), "default", meta.GetOptions{})
		return err == nil, nil
	}, d.tomb.Dying())
}

func (d *Detective) createPods() error {
	nodes, err := d.ListNodesWithPredicate(d.NodeIsSchedulabeleAndRunning)
	if err != nil {
		return err
	}

	//glog.V(2).Info("Creating pods")
	d.log.Info("creating pods")
	for _, node := range nodes {
		if !d.tomb.Alive() {
			return fmt.Errorf("Interrupted")
		}
		if _, err := d.createPod(d.createPodSpec(node, false)); err != nil {
			return err
		}
		if _, err := d.createPod(d.createPodSpec(node, true)); err != nil {
			return err
		}
	}

	return nil
}

func (d *Detective) createPod(pod *core.Pod) (*core.Pod, error) {
	pod, err := d.client.CoreV1().Pods(d.namespace.Name).Create(d.tomb.Context(nil), pod, meta.CreateOptions{})
	if err == nil {
		//glog.V(3).Infof("  created %v on %v", pod.Name, pod.Spec.NodeName)
		d.log.Info("created", "pod", pod.Name, "node", pod.Spec.NodeName)
	}
	return pod, err
}

func (d *Detective) createPodSpec(node *core.Node, hostNetwork bool) *core.Pod {
	var gracePeriod int64 = 2
	return &core.Pod{
		ObjectMeta: meta.ObjectMeta{
			GenerateName: "server-",
			Labels: map[string]string{
				"nodeName":    node.Name,
				"hostNetwork": strconv.FormatBool(hostNetwork),
			},
		},
		Spec: core.PodSpec{
			Containers: []core.Container{
				{
					Name:  "server",
					Image: d.testImage,
					Ports: []core.ContainerPort{{ContainerPort: 9376}},
				},
			},
			NodeName:                      node.Name,
			HostNetwork:                   hostNetwork,
			TerminationGracePeriodSeconds: &gracePeriod,
		},
	}
}

func (d *Detective) waitForPodsRunning() error {
	//glog.V(2).Info("Waiting for running Pods")
	d.log.Info("waiting for running pods")

	nodes, err := d.ListNodesWithPredicate(d.NodeIsSchedulabeleAndRunning)
	if err != nil {
		return err
	}

	return wait.PollImmediateUntil(1*time.Second, func() (done bool, err error) {
		pods, err := d.informers.Core().V1().Pods().Lister().Pods(d.namespace.Name).List(labels.Everything())
		if err != nil {
			return false, err
		}

		running := 0
		for _, pod := range pods {
			switch pod.Status.Phase {
			case core.PodRunning:
				running++
			case core.PodFailed:
				return false, fmt.Errorf("Failed to create a Pod: %v", pod.Status.Reason)
			}
		}

		//glog.V(3).Infof("  %v/%v pods running", running, len(nodes)*2)
		d.log.Info("pods", "running", running, "all pods", len(nodes)*2)
		return running == len(nodes)*2, nil
	}, d.tomb.Dying())
}

func (d *Detective) createSevices(withExternalIP bool) error {
	//glog.V(2).Info("Creating services")
	d.log.Info("creating services")

	pods, err := d.informers.Core().V1().Pods().Lister().Pods(d.namespace.Name).List(labels.Everything())
	if err != nil {
		return err
	}

	for _, pod := range pods {
		if !d.tomb.Alive() {
			return fmt.Errorf("Interrupted")
		}

		spec, err := d.createServiceSpec(pod, withExternalIP)
		if err != nil {
			return err
		}

		service, err := d.client.CoreV1().Services(d.namespace.Name).Create(d.tomb.Context(nil), spec, meta.CreateOptions{})
		if err != nil {
			return err
		}
		//glog.V(3).Infof("  created %v at %v for %v", service.Name, service.Spec.ExternalIPs, pod.Name)
		d.log.Info("created services", "name", service.Name, "ip", service.Spec.ExternalIPs, "pod", pod.Name)
	}

	return nil
}

func (d *Detective) createServiceSpec(pod *core.Pod, withExternalIP bool) (*core.Service, error) {
	service := &core.Service{
		ObjectMeta: meta.ObjectMeta{
			GenerateName: "clusterip-",
			Labels: map[string]string{
				"podName":     pod.Name,
				"podIP":       pod.Status.PodIP,
				"nodeName":    pod.Spec.NodeName,
				"hostNetwork": strconv.FormatBool(pod.Spec.HostNetwork),
			},
		},
		Spec: core.ServiceSpec{
			Type: core.ServiceTypeClusterIP,
			Ports: []core.ServicePort{
				{
					Port:       ServiceHttpPort,
					TargetPort: intstr.IntOrString{IntVal: PodHttpPort},
				},
			},
			Selector: map[string]string{
				"nodeName":    pod.Spec.NodeName,
				"hostNetwork": strconv.FormatBool(pod.Spec.HostNetwork),
			},
		},
	}

	if withExternalIP {
		if len(d.externalIPs) == 0 {
			return nil, fmt.Errorf("No more externalIPs available. Boom!")
		}

		d.externalIPs = d.externalIPs[1:]
		service.Spec.ExternalIPs = []string{d.externalIPs[0]}
	}

	return service, nil
}

func (d *Detective) waitForServiceEndpoints() error {
	//glog.V(2).Info("Waiting for service endpoints")
	d.log.Info("waiting for service endpoints")

	nodes, err := d.ListNodesWithPredicate(d.NodeIsSchedulabeleAndRunning)
	if err != nil {
		return err
	}

	return wait.PollImmediateUntil(1*time.Second, func() (done bool, err error) {
		services, err := d.informers.Core().V1().Services().Lister().List(labels.Everything())
		if err != nil {
			return false, err
		}

		//glog.V(3).Infof("  found %v nodes", len(nodes))
		d.log.Info("nodes", "found", len(nodes))
		//glog.V(3).Infof("  found %v srvices", len(services))
		d.log.Info("services", "found", len(services))

		ready := 0
		for _, service := range services {
			endpoints, err := d.informers.Core().V1().Endpoints().Lister().Endpoints(d.namespace.Name).Get(service.Name)
			if err != nil {
				if errors.IsNotFound(err) {
					//glog.V(3).Infof("  endpoint %v not found: %v", service.Name, err)
					d.log.Info("endpoint not found", "service", service.Name, "error", err)
					continue
				}
				//glog.V(3).Infof("  endpoint %v error: %v", service.Name, err)
				d.log.Info("endpoint error", "service", service.Name, "error", err)
				return false, err
			}
			for _, subset := range endpoints.Subsets {
				ready = ready + len(subset.Addresses)
			}
		}

		//glog.V(3).Infof("  %v/%v services ready", ready, len(nodes)*2)
		d.log.Info("services", "ready", ready, "all", len(nodes)*2)
		return ready == len(nodes)*2, nil
	}, d.tomb.Dying())
}
