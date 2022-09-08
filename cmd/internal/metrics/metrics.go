package metrics

import (
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promauto"
)

var TestTotal = promauto.NewCounterVec(prometheus.CounterOpts{
	Name: "kube_detective_tests_total",
	Help: "Number of total tests made",
}, []string{})

var ErrorTotal = promauto.NewCounterVec(prometheus.CounterOpts{
	Name: "kube_detective_error_total",
	Help: "Number of total errors",
}, []string{})

var PodIPTest = promauto.NewCounterVec(prometheus.CounterOpts{
	Name: "kube_detective_dial_pod_ip_total",
	Help: "Number of pod to pod tests",
}, []string{"source_node", "destination_node", "source_pod_ip", "destination_pod_ip"})

var PodIPTestError = promauto.NewCounterVec(prometheus.CounterOpts{
	Name: "kube_detective_dial_pod_ip_error_total",
	Help: "Number of pod to pod test errors",
}, []string{"source_node", "destination_node", "source_pod_ip", "destination_pod_ip"})

var ClusterIPTest = promauto.NewCounterVec(prometheus.CounterOpts{
	Name: "kube_detective_dial_cluster_ip_total",
	Help: "Number of pod to cluster ip tests",
}, []string{"source_node", "destination_node", "source_pod_ip", "destination_pod_ip", "cluster_ip"})

var ClusterIPTestError = promauto.NewCounterVec(prometheus.CounterOpts{
	Name: "kube_detective_dial_cluster_ip_error_total",
	Help: "Number of pod to cluster ip test errors",
}, []string{"source_node", "destination_node", "source_pod_ip", "destination_pod_ip", "cluster_ip"})

var ExternalIPTest = promauto.NewCounterVec(prometheus.CounterOpts{
	Name: "kube_detective_dial_external_ip_total",
	Help: "Number of pod to external ip tests",
}, []string{"source_node", "destination_node", "source_pod_ip", "destination_pod_ip", "external_ip"})

var ExternalIPTestError = promauto.NewCounterVec(prometheus.CounterOpts{
	Name: "kube_detective_dial_external_ip_error_total",
	Help: "Number of pod to external ip test errors",
}, []string{"source_node", "destination_node", "source_pod_ip", "destination_pod_ip", "external_ip"})