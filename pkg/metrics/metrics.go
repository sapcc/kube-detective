package metrics

import "github.com/prometheus/client_golang/prometheus"

var TestTotal = prometheus.NewCounterVec(prometheus.CounterOpts{
	Name: "kube_detective_tests_total",
	Help: "Number of total tests made",
}, []string{})

var ErrorTotal = prometheus.NewCounterVec(prometheus.CounterOpts{
	Name: "kube_detective_error_total",
	Help: "Number of total errors",
}, []string{})

var PodIPTest = prometheus.NewCounterVec(prometheus.CounterOpts{
	Name: "kube_detective_dial_pod_ip_total",
	Help: "Number of pod to pod tests",
}, []string{"source_node", "destination_node", "source_pod_ip", "destination_pod_ip"})

var ClusterIPTest = prometheus.NewCounterVec(prometheus.CounterOpts{
	Name: "kube_detective_dial_cluster_ip_total",
	Help: "Number of pod to cluster ip tests",
}, []string{"source_node", "destination_node", "source_pod_ip", "destination_pod_ip", "cluster_ip"})

var ExternalIPTest = prometheus.NewCounterVec(prometheus.CounterOpts{
	Name: "kube_detective_dial_external_ip_total",
	Help: "Number of pod to external ip tests",
}, []string{"source_node", "destination_node", "source_pod_ip", "destination_pod_ip", "external_ip"})
