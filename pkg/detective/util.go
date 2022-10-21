package detective

import (
	"net"

	core "k8s.io/api/core/v1"
	v1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/labels"
	"k8s.io/klog/v2"
)

func (d *Detective) NodeIsSchedulabeleAndRunning(node *core.Node) bool {
	if node.Spec.Unschedulable {
		return false
	}

	if len(node.Status.Conditions) == 0 {
		return false
	}

	if !d.nodeFilter.MatchString(node.Name) {
		return false
	}

	for _, cond := range node.Status.Conditions {
		if cond.Type == core.NodeReady && cond.Status != core.ConditionTrue {
			klog.V(3).Infof("Ignoring node %v with %v condition status %v", node.Name, cond.Type, cond.Status)
			return false
		}
	}
	return true
}

func (d *Detective) ListNodesWithPredicate(predicate func(node *v1.Node) bool) ([]*v1.Node, error) {
	nodes, err := d.informers.Core().V1().Nodes().Lister().List(labels.Everything())
	if err != nil {
		return nil, err
	}

	var filtered []*v1.Node
	for i := range nodes {
		if predicate(nodes[i]) {
			filtered = append(filtered, nodes[i])
		}
	}

	return filtered, nil
}

func inc(ip net.IP) {
	for j := len(ip) - 1; j >= 0; j-- {
		ip[j]++
		if ip[j] > 0 {
			break
		}
	}
}
