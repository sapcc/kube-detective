package detective

import (
	"fmt"
	"strconv"

	"github.com/golang/glog"
	"github.com/kubernetes/kubernetes/staging/src/k8s.io/client-go/util/workqueue"
	core "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/labels"
)

const (
	WORKER_COUNT = 10
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

	workqueue.ParallelizeUntil(d.tomb.Context(nil), WORKER_COUNT, len(services)*len(pods), func(i int) {
		pod := targets[i].source
		service := targets[i].target
		if sourceHostNetwork == pod.Spec.HostNetwork {
			if s, err := strconv.ParseBool(service.Labels["hostNetwork"]); err == nil && targetHostNetwork == s {
				d.dialClusterIP(pod, service)
			}
		}
	})

	return nil
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

	workqueue.ParallelizeUntil(ctx, WORKER_COUNT, len(services)*len(pods), func(i int) {
		pod := targets[i].source
		service := targets[i].target
		if sourceHostNetwork == pod.Spec.HostNetwork {
			if s, err := strconv.ParseBool(service.Labels["hostNetwork"]); err == nil && targetHostNetwork == s {
				d.dialExternalIP(pod, service)
			}
		}
	})

	return ctx.Err()
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

	workqueue.ParallelizeUntil(ctx, WORKER_COUNT, len(pods)*len(pods), func(i int) {
		source := targets[i].source
		target := targets[i].target
		if sourceHostNetwork == source.Spec.HostNetwork && targetHostNetwork == target.Spec.HostNetwork {
			d.dialPodIP(source, target)
		}
	})

	return ctx.Err()
}

func (d *Detective) dialPodIP(source *core.Pod, target *core.Pod) {
	_, err := d.dial(source, target.Status.PodIP, PodHttpPort)

	result := "success"
	if err != nil {
		glog.V(3).Infof("Error: '%v'", err)
		result = "failure"
	}

	fmt.Printf("[%v] %30v --> %-30v   %-15v --> %-15v\n",
		result,
		source.Spec.NodeName,
		target.Spec.NodeName,
		source.Status.PodIP,
		target.Status.PodIP,
	)
}

func (d *Detective) dialClusterIP(pod *core.Pod, service *core.Service) {
	_, err := d.dial(pod, service.Spec.ClusterIP, service.Spec.Ports[0].Port)

	result := "success"
	if err != nil {
		glog.V(3).Infof("Error: '%s'", err)

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
}

func (d *Detective) dialExternalIP(pod *core.Pod, service *core.Service) {
	_, err := d.dial(pod, service.Spec.ExternalIPs[0], service.Spec.Ports[0].Port)

	result := "success"
	if err != nil {
		glog.V(3).Infof("Error: '%s'", err)

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
}

func (d *Detective) dial(pod *core.Pod, host string, port int32) (string, error) {
	cmd := fmt.Sprintf("wget --timeout=10 -O - http://%v:%v", host, port)
	return RunHostCmd(d.tomb.Context(nil), d.namespace.Name, pod.Name, cmd)
}
