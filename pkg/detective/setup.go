package detective

import (
	"fmt"
	"os"
	"strconv"
	"sync"

	"github.com/golang/glog"

	"k8s.io/client-go/1.5/kubernetes"
	"k8s.io/client-go/1.5/pkg/api"
	"k8s.io/client-go/1.5/pkg/api/errors"
	"k8s.io/client-go/1.5/pkg/api/unversioned"
	"k8s.io/client-go/1.5/pkg/api/v1"
	"k8s.io/client-go/1.5/pkg/fields"
	"k8s.io/client-go/1.5/pkg/util/intstr"
	"k8s.io/client-go/1.5/pkg/util/wait"
	"k8s.io/client-go/1.5/pkg/watch"
	"k8s.io/client-go/1.5/tools/clientcmd"
)

func (d *Detective) handleError(err error) {
	if err != nil {
		fmt.Println(fmt.Sprintf("An error occured: %v\n", err))
		d.cleanup()
		os.Exit(-1)
	}
}

func (d *Detective) createClient() {
	glog.V(2).Infof("Creating Client")
	rules := clientcmd.NewDefaultClientConfigLoadingRules()
	overrides := &clientcmd.ConfigOverrides{}

	config, err := clientcmd.NewNonInteractiveDeferredLoadingClientConfig(rules, overrides).ClientConfig()
	d.handleError(err)

	client, err := kubernetes.NewForConfig(config)
	d.handleError(err)

	d.client = client
	glog.V(3).Infof("  using %s", config.Host)
}

func (d *Detective) createNamespace() {
	glog.V(2).Infof("Creating Namespace")
	spec := &v1.Namespace{
		ObjectMeta: v1.ObjectMeta{
			GenerateName: "detective-",
		},
		Status: v1.NamespaceStatus{},
	}

	ns, err := d.client.Namespaces().Create(spec)
	d.handleError(err)

	d.namespace = ns
	glog.V(3).Infof("  created %v", ns.Name)
}

func (d *Detective) deleteNamespace() {
	glog.V(2).Infof("Deleting Namespace")
	err := d.client.Namespaces().Delete(d.namespace.Name, api.NewDeleteOptions(0))
	d.handleError(err)
	glog.V(3).Infof("  deleted %v", d.namespace.Name)
}

func (d *Detective) waitForServiceAccountInNamespace() {
	glog.V(2).Info("Waiting for Service Account")
	w, err := d.client.ServiceAccounts(d.namespace.Name).Watch(api.SingleObject(api.ObjectMeta{Name: "default"}))
	d.handleError(err)

	_, err = watch.Until(ServiceAccountProvisionTimeout, w, ServiceAccountHasSecrets)
	d.handleError(err)
	glog.V(3).Infof("  available %v", "default")
}

func ServiceAccountHasSecrets(event watch.Event) (bool, error) {
	switch event.Type {
	case watch.Deleted:
		return false, errors.NewNotFound(unversioned.GroupResource{Resource: "serviceaccounts"}, "")
	}
	switch t := event.Object.(type) {
	case *v1.ServiceAccount:
		return len(t.Secrets) > 0, nil
	}
	return false, nil
}

func PodRunning(event watch.Event) (bool, error) {
	switch event.Type {
	case watch.Deleted:
		return false, errors.NewNotFound(unversioned.GroupResource{Resource: "pods"}, "")
	}
	switch t := event.Object.(type) {
	case *v1.Pod:
		switch t.Status.Phase {
		case v1.PodRunning:
			return true, nil
		case v1.PodFailed, v1.PodSucceeded:
			return false, fmt.Errorf("pod failed or ran to completion")
		}
	}
	return false, nil
}

func (d *Detective) getReadySchedulableNodes() {
	glog.V(2).Info("Finding schedulable and ready nodes")

	opts := api.ListOptions{
		ResourceVersion: "0",
		FieldSelector:   fields.Set{"spec.unschedulable": "false"}.AsSelector(),
	}
	nodes, err := d.client.Nodes().List(opts)
	d.handleError(err)

	filterNodes(nodes, func(node v1.Node) bool {
		return isNodeConditionSetAsExpected(&node, v1.NodeReady, true)
	})

	d.nodes = nodes.Items

	for _, node := range nodes.Items {
		glog.V(3).Infof("  found %v", node.Name)
	}
}

func (d *Detective) createPods() {
	glog.V(2).Info("Creating pods")

	specs := make([]*v1.Pod, 0)
	for _, node := range d.nodes {
		specs = append(specs, d.createPodSpec(node, false), d.createPodSpec(node, true))
	}

	d.pods = d.createPodsInBatch(specs)
}

func (d *Detective) createPodsInBatch(pods []*v1.Pod) []*v1.Pod {
	ps := make([]*v1.Pod, len(pods))
	var wg sync.WaitGroup
	for i, pod := range pods {
		wg.Add(1)
		go func(i int, pod *v1.Pod) {
			defer wg.Done()
			ps[i] = d.createPodAndWait(pod)
		}(i, pod)
	}
	wg.Wait()
	return ps
}

func (d *Detective) createPodAndWait(pod *v1.Pod) *v1.Pod {
	pod = d.CreatePod(pod)
	d.WaitForPodRunning(pod)
	return d.RefreshPod(pod)
}

func (d *Detective) CreatePod(pod *v1.Pod) *v1.Pod {
	pod, err := d.client.Pods(d.namespace.Name).Create(pod)
	d.handleError(err)
	glog.V(3).Infof("  created %v on %v", pod.Name, pod.Spec.NodeName)
	return pod
}

func (d *Detective) RefreshPod(pod *v1.Pod) *v1.Pod {
	glog.V(3).Infof("  refreshing %v", pod.Name)
	p, err := d.client.Pods(d.namespace.Name).Get(pod.Name)
	d.handleError(err)
	return p
}

func (d *Detective) WaitForPodRunning(pod *v1.Pod) {
	glog.V(3).Infof("  waiting for %v on %v", pod.Name, pod.Spec.NodeName)
	w, err := d.client.Pods(d.namespace.Name).Watch(api.SingleObject(api.ObjectMeta{Name: pod.Name}))
	d.handleError(err)

	_, err = watch.Until(PodStartTimeout, w, PodRunning)
	d.handleError(err)
	glog.V(2).Infof("  running %v on %v", pod.Name, pod.Spec.NodeName)
}

func (d *Detective) createPodSpec(node v1.Node, hostNetwork bool) *v1.Pod {
	return &v1.Pod{
		ObjectMeta: v1.ObjectMeta{
			GenerateName: "server-",
			Labels: map[string]string{
				"nodeName":    node.Name,
				"hostNetwork": strconv.FormatBool(hostNetwork),
			},
		},
		Spec: v1.PodSpec{
			Containers: []v1.Container{
				{
					Name:  "server",
					Image: "gcr.io/google_containers/serve_hostname:1.2",
					Ports: []v1.ContainerPort{{ContainerPort: 9376}},
				},
			},
			NodeName:    node.Name,
			HostNetwork: hostNetwork,
		},
	}
}

func (d *Detective) createServices() {
	glog.V(2).Info("Creating services")
	specs := make([]*v1.Service, 0)
	for _, pod := range d.pods {
		specs = append(specs, d.createServiceSpec(pod))
	}

	d.services = d.createServicesInBatch(specs)
}

func (d *Detective) createServicesInBatch(services []*v1.Service) []*v1.Service {
	ps := make([]*v1.Service, len(services))
	var wg sync.WaitGroup
	for i, service := range services {
		wg.Add(1)
		go func(i int, service *v1.Service) {
			defer wg.Done()
			ps[i] = d.createServiceAndWait(service)
		}(i, service)
	}
	wg.Wait()
	return ps
}

func (d *Detective) createServiceAndWait(spec *v1.Service) *v1.Service {
	service, err := d.client.Services(d.namespace.Name).Create(spec)
	d.handleError(err)
	glog.V(3).Infof("  created %v", service.Name)

	err = wait.Poll(WaitForEndpointInterval, WaitForEndpointTimeout, d.ServiceHasDesiredEndpoints(service, 1))
	d.handleError(err)
	glog.V(3).Infof("  ready %v", service.Name)

	service, err = d.client.Services(d.namespace.Name).Get(service.Name)
	d.handleError(err)
	glog.V(3).Infof("  refreshed %v", service.Name)

	return service
}

func (d *Detective) createServiceSpec(pod *v1.Pod) *v1.Service {
	if len(d.externalIPs) == 0 {
		d.handleError(fmt.Errorf("No more externalIPs available. Boom!"))
	}

	externalIP := d.externalIPs[0]
	d.externalIPs = d.externalIPs[1:]
	glog.V(3).Infof("  externalIP %v", externalIP)

	return &v1.Service{
		ObjectMeta: v1.ObjectMeta{
			GenerateName: "clusterip-",
			Labels: map[string]string{
				"podName":     pod.Name,
				"podIP":       pod.Status.PodIP,
				"nodeName":    pod.Spec.NodeName,
				"hostNetwork": strconv.FormatBool(pod.Spec.HostNetwork),
			},
		},
		Spec: v1.ServiceSpec{
			Type: v1.ServiceTypeClusterIP,
			Ports: []v1.ServicePort{
				{
					Port:       ServiceHttpPort,
					TargetPort: intstr.IntOrString{IntVal: PodHttpPort},
				},
			},
			Selector: map[string]string{
				"nodeName":    pod.Spec.NodeName,
				"hostNetwork": strconv.FormatBool(pod.Spec.HostNetwork),
			},
			ExternalIPs: []string{externalIP},
		},
	}
}

func (d *Detective) ServiceHasDesiredEndpoints(service *v1.Service, desired int) wait.ConditionFunc {
	return func() (bool, error) {
		endpoints, err := d.client.Endpoints(d.namespace.Name).Get(service.Name)
		if err != nil {
			return false, err
		}

		actual := 0
		for _, subset := range endpoints.Subsets {
			actual = actual + len(subset.Addresses)
		}

		return actual >= desired, nil
	}
}
