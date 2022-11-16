package main

import (
	"flag"
	"fmt"
	"os"
	"os/signal"
	"syscall"

	"github.com/hashicorp/go-multierror"
	"github.com/sapcc/kube-detective/pkg/detective"
	"k8s.io/client-go/tools/clientcmd"
	"k8s.io/klog/v2"
)

var (
	opts       detective.Options
	kubeconfig string
	context    string
)

func init() {
	flag.StringVar(&opts.ExternalCIDR, "externalCIDR", "", "subnet used for external IPs")
	flag.StringVar(&opts.NodeFilterRegex, "nodeFilter", ".*", "filter node names with this regex")
	flag.BoolVar(&opts.TestPods, "pods", true, "test pods")
	flag.BoolVar(&opts.TestServices, "services", true, "test services")
	flag.BoolVar(&opts.TestExternalIPs, "externalips", false, "test external IPs")
	flag.BoolVar(&opts.TestServiceName, "service-name", true, "test service name resolution from each pod")
	flag.IntVar(&opts.WorkerCount, "workers", 10, "Number of workers to run checks in parallel")
	flag.StringVar(&opts.TestImage, "test-image", "gcr.io/google_containers/serve_hostname:1.2", "test external IPs")
	flag.StringVar(&kubeconfig, "kubeconfig", "", "Explicit kubeconfig (default: $KUBECONFIG)")
	flag.StringVar(&context, "context", os.Getenv("KUBECONTEXT"), "context to use from kubeconfig (default: $KUBECONTEXT, current-context)")
}

func main() {
	klog.InitFlags(nil)
	flag.Parse()

	sigs := make(chan os.Signal, 1)
	signal.Notify(sigs, os.Interrupt, syscall.SIGTERM)

	rules := clientcmd.NewDefaultClientConfigLoadingRules()
	overrides := &clientcmd.ConfigOverrides{CurrentContext: context}
	rules.ExplicitPath = kubeconfig
	var err error
	opts.RestConfig, err = clientcmd.NewNonInteractiveDeferredLoadingClientConfig(rules, overrides).ClientConfig()
	if err != nil {
		fmt.Printf("Failed to load kube config: %v\n", err)
		os.Exit(1)
	}

	d := detective.NewDetective(opts)

	go func() {
		<-sigs
		d.Kill()
	}()

	if err := d.Wait(); err != nil {
		if merr, ok := err.(*multierror.Error); ok {
			fmt.Printf("Encountered %d errors while running tests\n", merr.Len())
		} else {
			fmt.Printf("Error: %v\n", err)

		}
		os.Exit(1)
	}
}
