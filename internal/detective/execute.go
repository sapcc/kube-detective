package detective

import (
	"fmt"
	"github.com/sapcc/kube-detective/internal/metrics"
	"strconv"

	"github.com/golang/glog"
	core "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/labels"
	"k8s.io/client-go/util/workqueue"
)

const (
	WORKER_COUNT = 10
)

var (
	sourceNode string
	destNode   string
	sourcePod  string
	destPod    string
	clusterIP  string
	externalIP string
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

	sourceNode = source.Spec.NodeName
	destNode = target.Spec.NodeName
	sourcePod = source.Status.PodIP
	destPod = target.Status.PodIP

	result := "success"
	metrics.TestTotal.WithLabelValues().Inc()
	metrics.PodIPTest.WithLabelValues(sourceNode, destNode, sourcePod, destPod).Inc()
	if err != nil {
		metrics.ErrorTotal.WithLabelValues().Inc()
		metrics.PodIPTestError.WithLabelValues(sourceNode, destNode, sourcePod, destPod).Inc()

		glog.V(3).Infof("Error: '%v'", err)
		result = "failure"
	}

	fmt.Printf("[%v] %30v --> %-30v   %-15v --> %-15v\n", result, sourceNode, destNode, sourcePod, destPod)
}

func (d *Detective) dialClusterIP(pod *core.Pod, service *core.Service) {
	_, err := d.dial(pod, service.Spec.ClusterIP, service.Spec.Ports[0].Port)

	sourceNode = pod.Spec.NodeName
	destNode = service.Labels["nodeName"]
	sourcePod = pod.Status.PodIP
	destPod = service.Labels["podIP"]
	clusterIP = service.Spec.ClusterIP

	result := "success"
	metrics.TestTotal.WithLabelValues().Inc()
	metrics.ClusterIPTest.WithLabelValues(sourceNode, destNode, sourcePod, destPod, clusterIP).Inc()

	if err != nil {
		glog.V(3).Infof("Error: '%s'", err)

		metrics.ErrorTotal.WithLabelValues().Inc()
		metrics.ClusterIPTestError.WithLabelValues(sourceNode, destNode, sourcePod, destPod, clusterIP).Inc()
		result = "failure"
	}

	fmt.Printf("[%v] %30v --> ClusterIP --> %-30v   %-15v --> %-15v --> %-15v\n",
		result, sourceNode, destNode, sourcePod, clusterIP, destPod,
	)
}

func (d *Detective) dialExternalIP(pod *core.Pod, service *core.Service) {
	_, err := d.dial(pod, service.Spec.ExternalIPs[0], service.Spec.Ports[0].Port)

	sourceNode = pod.Spec.NodeName
	destNode = service.Labels["nodeName"]
	sourcePod = pod.Status.PodIP
	destPod = service.Labels["podIP"]
	externalIP = service.Spec.ExternalIPs[0]

	result := "success"
	metrics.TestTotal.WithLabelValues().Inc()
	metrics.ExternalIPTest.WithLabelValues(sourceNode, destNode, sourcePod, destPod, externalIP).Inc()

	if err != nil {
		glog.V(3).Infof("Error: '%s'", err)

		metrics.ErrorTotal.WithLabelValues().Inc()
		metrics.ExternalIPTestError.WithLabelValues(sourceNode, destNode, sourcePod, destPod, externalIP).Inc()
		result = "failure"
	}

	fmt.Printf("[%v] %30v --> ExternalIP --> %-30v   %-15v --> %-15v --> %-15v\n",
		result, sourceNode, destNode, sourcePod, externalIP, destPod,
	)
}

func (d *Detective) dial(pod *core.Pod, host string, port int32) (string, error) {
	cmd := fmt.Sprintf("wget --timeout=10 -O - http://%v:%v", host, port)
	return RunHostCmd(d.tomb.Context(nil), d.namespace.Name, pod.Name, cmd)
}
