package detective

import (
	"fmt"
	"os"
	"strconv"
	"sync"

	"github.com/golang/glog"

	core "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	meta "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/fields"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/util/intstr"
	"k8s.io/apimachinery/pkg/util/wait"
	"k8s.io/apimachinery/pkg/watch"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/tools/clientcmd"
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
	spec := &core.Namespace{
		ObjectMeta: meta.ObjectMeta{
			GenerateName: "detective-",
		},
		Status: core.NamespaceStatus{},
	}

	ns, err := d.client.CoreV1().Namespaces().Create(spec)
	d.handleError(err)

	d.namespace = ns
	glog.V(3).Infof("  created %v", ns.Name)
}

func (d *Detective) deleteNamespace() {
	glog.V(2).Infof("Deleting Namespace")
	err := d.client.CoreV1().Namespaces().Delete(d.namespace.Name, meta.NewDeleteOptions(0))
	d.handleError(err)
	glog.V(3).Infof("  deleted %v", d.namespace.Name)
}

func (d *Detective) waitForServiceAccountInNamespace() {
	glog.V(2).Info("Waiting for Service Account")
	w, err := d.client.CoreV1().ServiceAccounts(d.namespace.Name).Watch(meta.SingleObject(meta.ObjectMeta{Name: "default"}))
	d.handleError(err)

	_, err = watch.Until(ServiceAccountProvisionTimeout, w, ServiceAccountHasSecrets)
	d.handleError(err)
	glog.V(3).Infof("  available %v", "default")
}

func ServiceAccountHasSecrets(event watch.Event) (bool, error) {
	switch event.Type {
	case watch.Deleted:
		return false, errors.NewNotFound(schema.GroupResource{Resource: "serviceaccounts"}, "")
	}
	switch t := event.Object.(type) {
	case *core.ServiceAccount:
		return len(t.Secrets) > 0, nil
	}
	return false, nil
}

func PodRunning(event watch.Event) (bool, error) {
	switch event.Type {
	case watch.Deleted:
		return false, errors.NewNotFound(schema.GroupResource{Resource: "pods"}, "")
	}
	switch t := event.Object.(type) {
	case *core.Pod:
		switch t.Status.Phase {
		case core.PodRunning:
			return true, nil
		case core.PodFailed, core.PodSucceeded:
			return false, fmt.Errorf("pod failed or ran to completion")
		}
	}
	return false, nil
}

func (d *Detective) getReadySchedulableNodes() {
	glog.V(2).Info("Finding schedulable and ready nodes")

	opts := meta.ListOptions{
		ResourceVersion: "0",
		FieldSelector:   fields.Set{"spec.unschedulable": "false"}.AsSelector().String(),
	}
	nodes, err := d.client.CoreV1().Nodes().List(opts)
	d.handleError(err)

	filterNodes(nodes, func(node core.Node) bool {
		return isNodeConditionSetAsExpected(&node, core.NodeReady, true)
	})

	d.nodes = nodes.Items

	for _, node := range nodes.Items {
		glog.V(3).Infof("  found %v", node.Name)
	}
}

func (d *Detective) createPods() {
	glog.V(2).Info("Creating pods")

	specs := make([]*core.Pod, 0)
	for _, node := range d.nodes {
		specs = append(specs, d.createPodSpec(node, false), d.createPodSpec(node, true))
	}

	d.pods = d.createPodsInBatch(specs)
}

func (d *Detective) createPodsInBatch(pods []*core.Pod) []*core.Pod {
	ps := make([]*core.Pod, len(pods))
	var wg sync.WaitGroup
	for i, pod := range pods {
		wg.Add(1)
		go func(i int, pod *core.Pod) {
			defer wg.Done()
			ps[i] = d.createPodAndWait(pod)
		}(i, pod)
	}
	wg.Wait()
	return ps
}

func (d *Detective) createPodAndWait(pod *core.Pod) *core.Pod {
	pod = d.CreatePod(pod)
	d.WaitForPodRunning(pod)
	return d.RefreshPod(pod)
}

func (d *Detective) CreatePod(pod *core.Pod) *core.Pod {
	pod, err := d.client.CoreV1().Pods(d.namespace.Name).Create(pod)
	d.handleError(err)
	glog.V(3).Infof("  created %v on %v", pod.Name, pod.Spec.NodeName)
	return pod
}

func (d *Detective) RefreshPod(pod *core.Pod) *core.Pod {
	glog.V(3).Infof("  refreshing %v", pod.Name)
	p, err := d.client.CoreV1().Pods(d.namespace.Name).Get(pod.Name, meta.GetOptions{})
	d.handleError(err)
	return p
}

func (d *Detective) WaitForPodRunning(pod *core.Pod) {
	glog.V(3).Infof("  waiting for %v on %v", pod.Name, pod.Spec.NodeName)
	w, err := d.client.CoreV1().Pods(d.namespace.Name).Watch(meta.SingleObject(meta.ObjectMeta{Name: pod.Name}))
	d.handleError(err)

	_, err = watch.Until(PodStartTimeout, w, PodRunning)
	d.handleError(err)
	glog.V(2).Infof("  running %v on %v", pod.Name, pod.Spec.NodeName)
}

func (d *Detective) createPodSpec(node core.Node, hostNetwork bool) *core.Pod {
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
					Image: "gcr.io/google_containers/serve_hostname:1.2",
					Ports: []core.ContainerPort{{ContainerPort: 9376}},
				},
			},
			NodeName:    node.Name,
			HostNetwork: hostNetwork,
		},
	}
}

func (d *Detective) createServices() {
	glog.V(2).Info("Creating services")
	specs := make([]*core.Service, 0)
	for _, pod := range d.pods {
		specs = append(specs, d.createServiceSpec(pod))
	}

	d.services = d.createServicesInBatch(specs)
}

func (d *Detective) createServicesInBatch(services []*core.Service) []*core.Service {
	ps := make([]*core.Service, len(services))
	var wg sync.WaitGroup
	for i, service := range services {
		wg.Add(1)
		go func(i int, service *core.Service) {
			defer wg.Done()
			ps[i] = d.createServiceAndWait(service)
		}(i, service)
	}
	wg.Wait()
	return ps
}

func (d *Detective) createServiceAndWait(spec *core.Service) *core.Service {
	service, err := d.client.CoreV1().Services(d.namespace.Name).Create(spec)
	d.handleError(err)
	glog.V(3).Infof("  created %v", service.Name)

	err = wait.Poll(WaitForEndpointInterval, WaitForEndpointTimeout, d.ServiceHasDesiredEndpoints(service, 1))
	d.handleError(err)
	glog.V(3).Infof("  ready %v", service.Name)

	service, err = d.client.CoreV1().Services(d.namespace.Name).Get(service.Name, meta.GetOptions{})
	d.handleError(err)
	glog.V(3).Infof("  refreshed %v", service.Name)

	return service
}

func (d *Detective) createServiceSpec(pod *core.Pod) *core.Service {
	if len(d.externalIPs) == 0 {
		d.handleError(fmt.Errorf("No more externalIPs available. Boom!"))
	}

	externalIP := d.externalIPs[0]
	d.externalIPs = d.externalIPs[1:]
	glog.V(3).Infof("  externalIP %v", externalIP)

	return &core.Service{
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
			ExternalIPs: []string{externalIP},
		},
	}
}

func (d *Detective) ServiceHasDesiredEndpoints(service *core.Service, desired int) wait.ConditionFunc {
	return func() (bool, error) {
		endpoints, err := d.client.CoreV1().Endpoints(d.namespace.Name).Get(service.Name, meta.GetOptions{})
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
