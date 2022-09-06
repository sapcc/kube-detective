package main

import (
	"flag"
	"fmt"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/push"
	"os"
	"os/signal"
	"syscall"

	"github.com/sapcc/kube-detective/pkg/detective"
	"github.com/sapcc/kube-detective/pkg/metrics"
	"k8s.io/klog/v2"
)

var opts detective.Options

func init() {
	flag.StringVar(&opts.ExternalCIDR, "externalCIDR", "", "subnet used for external IPs")
	flag.StringVar(&opts.NodeFilterRegex, "nodeFilter", ".*", "filter node names with this regex")
	flag.BoolVar(&opts.TestPods, "pods", true, "test pods")
	flag.BoolVar(&opts.TestServices, "services", true, "test services")
	flag.BoolVar(&opts.TestExternalIPs, "externalips", false, "test external IPs")
	flag.StringVar(&opts.TestImage, "test-image", "gcr.io/google_containers/serve_hostname:1.2", "test external IPs")
	flag.StringVar(&opts.PushGateway, "pushgateway", "pushgateway.local:9091", "pushgateway url")
}

func main() {
	flag.Set("alsologtostderr", "true")
	flag.Parse()

	registry := prometheus.NewRegistry()
	registry.MustRegister(metrics.TestTotal, metrics.ErrorTotal, metrics.PodIPTest, metrics.PodIPTestError,
		metrics.ClusterIPTest, metrics.ClusterIPTestError, metrics.ExternalIPTest, metrics.ExternalIPTestError)

	pusher := push.New(opts.PushGateway, "kube_detective").Gatherer(registry)

	klogFlags := flag.NewFlagSet("klog", flag.ExitOnError)
	klog.InitFlags(klogFlags)

	sigs := make(chan os.Signal, 1)
	signal.Notify(sigs, os.Interrupt, syscall.SIGTERM)

	d := detective.NewDetective(opts)

	go func() {
		<-sigs
		d.Kill()
	}()

	if err := d.Wait(); err != nil {
		fmt.Printf("Error: %v\n", err)
		os.Exit(1)
	}

	if err := pusher.Add(); err != nil {
		fmt.Println("Could not push to Pushgateway:", err)
	}
	fmt.Printf("Pushed metrics to Pushgateway: %s", opts.PushGateway)
}
