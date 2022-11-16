package detective

import (
	"fmt"
	"strconv"
	"sync"

	"github.com/hashicorp/go-multierror"
	core "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/labels"
	"k8s.io/client-go/util/workqueue"
	"k8s.io/klog/v2"
)

type ServiceTarget struct {
	source *core.Pod
	target *core.Service
}

type PodTarget struct {
	source *core.Pod
	target *core.Pod
}

func (d *Detective) hitServices(sourceHostNetwork, targetHostNetwork bool) error {
	services, err := d.informers.Core().V1().Services().Lister().Services(d.namespace.Name).List(labels.Everything())
	if err != nil {
		return err
	}

	pods, err := d.informers.Core().V1().Pods().Lister().Pods(d.namespace.Name).List(labels.Everything())
	if err != nil {
		return err
	}

	targets := []ServiceTarget{}
	for _, service := range services {
		for _, pod := range pods {
			if !d.tomb.Alive() {
				return fmt.Errorf("Interrupted")
			}
			targets = append(targets, ServiceTarget{pod, service})
		}
	}

	var result *multierror.Error
	var mutex sync.Mutex

	workqueue.ParallelizeUntil(d.tomb.Context(nil), d.workerCount, len(services)*len(pods), func(i int) {
		pod := targets[i].source
		service := targets[i].target
		if sourceHostNetwork == pod.Spec.HostNetwork {
			if s, err := strconv.ParseBool(service.Labels["hostNetwork"]); err == nil && targetHostNetwork == s {
				err := d.dialClusterIP(pod, service)
				mutex.Lock()
				result = multierror.Append(result, err)
				mutex.Unlock()
			}
		}
	})

	return result.ErrorOrNil()
}

func (d *Detective) hitServiceName() error {
	services, err := d.informers.Core().V1().Services().Lister().Services(d.namespace.Name).List(labels.Everything())
	if err != nil {
		return err
	}

	pods, err := d.informers.Core().V1().Pods().Lister().Pods(d.namespace.Name).List(labels.Everything())
	if err != nil {
		return err
	}

	targets := []ServiceTarget{}
	//for each pod we test a single service name resolution
	for _, pod := range pods {
		if !d.tomb.Alive() {
			return fmt.Errorf("Interrupted")
		}
		if pod.Spec.HostNetwork { // skip host networking pods
			continue
		}
		targets = append(targets, ServiceTarget{pod, services[0]})
	}

	var result *multierror.Error
	var mutex sync.Mutex

	workqueue.ParallelizeUntil(d.tomb.Context(nil), d.workerCount, len(targets), func(i int) {
		pod := targets[i].source
		service := targets[i].target
		err := d.dialServiceDNS(pod, service)
		mutex.Lock()
		result = multierror.Append(result, err)
		mutex.Unlock()
	})

	return result.ErrorOrNil()
}

func (d *Detective) hitExternalIP(sourceHostNetwork, targetHostNetwork bool) error {
	services, err := d.informers.Core().V1().Services().Lister().Services(d.namespace.Name).List(labels.Everything())
	if err != nil {
		return err
	}

	pods, err := d.informers.Core().V1().Pods().Lister().Pods(d.namespace.Name).List(labels.Everything())
	if err != nil {
		return err
	}

	targets := []ServiceTarget{}
	for _, service := range services {
		for _, pod := range pods {
			if !d.tomb.Alive() {
				return fmt.Errorf("Interrupted")
			}
			targets = append(targets, ServiceTarget{pod, service})
		}
	}

	ctx := d.tomb.Context(nil)

	var result *multierror.Error
	var mutex sync.Mutex

	workqueue.ParallelizeUntil(ctx, d.workerCount, len(services)*len(pods), func(i int) {
		pod := targets[i].source
		service := targets[i].target
		if sourceHostNetwork == pod.Spec.HostNetwork {
			if s, err := strconv.ParseBool(service.Labels["hostNetwork"]); err == nil && targetHostNetwork == s {
				err := d.dialExternalIP(pod, service)
				mutex.Lock()
				result = multierror.Append(result, err)
				mutex.Unlock()
			}
		}
	})

	return multierror.Append(result, ctx.Err()).ErrorOrNil()
}

func (d *Detective) hitPods(sourceHostNetwork, targetHostNetwork bool) error {
	pods, err := d.informers.Core().V1().Pods().Lister().Pods(d.namespace.Name).List(labels.Everything())
	if err != nil {
		return err
	}

	targets := []PodTarget{}
	for _, source := range pods {
		for _, target := range pods {
			if !d.tomb.Alive() {
				return fmt.Errorf("Interrupted")
			}
			targets = append(targets, PodTarget{source, target})
		}
	}

	ctx := d.tomb.Context(nil)
	var result *multierror.Error
	var mutex sync.Mutex

	workqueue.ParallelizeUntil(ctx, d.workerCount, len(pods)*len(pods), func(i int) {
		source := targets[i].source
		target := targets[i].target
		if sourceHostNetwork == source.Spec.HostNetwork && targetHostNetwork == target.Spec.HostNetwork {
			err := d.dialPodIP(source, target)
			mutex.Lock()
			result = multierror.Append(result, err)
			mutex.Unlock()
		}
	})

	return multierror.Append(result, ctx.Err()).ErrorOrNil()
}

func (d *Detective) dialPodIP(source *core.Pod, target *core.Pod) error {
	_, err := d.dial(source, target.Status.PodIP, PodHttpPort)

	result := "success"
	if err != nil {
		klog.V(3).Infof("Error: '%v'", err)
		result = "failure"
	}

	fmt.Printf("[%v] %30v --> %-30v   %-15v --> %-15v\n",
		result,
		source.Spec.NodeName,
		target.Spec.NodeName,
		source.Status.PodIP,
		target.Status.PodIP,
	)
	return err
}

func (d *Detective) dialClusterIP(pod *core.Pod, service *core.Service) error {
	_, err := d.dial(pod, service.Spec.ClusterIP, service.Spec.Ports[0].Port)

	result := "success"
	if err != nil {
		klog.V(3).Infof("Error: '%s'", err)

		result = "failure"
	}

	fmt.Printf("[%v] %30v --> ClusterIP --> %-30v   %-15v --> %-15v --> %-15v\n",
		result,
		pod.Spec.NodeName,
		service.Labels["nodeName"],
		pod.Status.PodIP,
		service.Spec.ClusterIP,
		service.Labels["podIP"],
	)
	return err
}

func (d *Detective) dialServiceDNS(pod *core.Pod, service *core.Service) error {
	_, err := d.dial(pod, service.Name, service.Spec.Ports[0].Port)

	result := "success"
	if err != nil {
		klog.V(3).Infof("Error: '%s'", err)

		result = "failure"
	}

	fmt.Printf("[%v] %30v --> Service Name    %-15v --> %-15v --> %-15v\n",
		result,
		pod.Spec.NodeName,
		pod.Status.PodIP,
		service.Name,
		service.Labels["podIP"],
	)
	return err
}

func (d *Detective) dialExternalIP(pod *core.Pod, service *core.Service) error {
	_, err := d.dial(pod, service.Spec.ExternalIPs[0], service.Spec.Ports[0].Port)

	result := "success"
	if err != nil {
		klog.V(3).Infof("Error: '%s'", err)

		result = "failure"
	}

	fmt.Printf("[%v] %30v --> ExternalIP --> %-30v   %-15v --> %-15v --> %-15v\n",
		result,
		pod.Spec.NodeName,
		service.Labels["nodeName"],
		pod.Status.PodIP,
		service.Spec.ExternalIPs[0],
		service.Labels["podIP"],
	)
	return err
}

func (d *Detective) dial(pod *core.Pod, host string, port int32) (string, error) {
	stdout, stderr, err := d.ExecWithOptions(ExecOptions{
		Command:            []string{"wget", "--timeout=10", "-O-", fmt.Sprintf("http://%v:%v", host, port)},
		Namespace:          d.namespace.Name,
		PodName:            pod.Name,
		ContainerName:      "server",
		CaptureStderr:      true,
		CaptureStdout:      true,
		PreserveWhitespace: true,
	})
	return stdout + stderr, err
}
