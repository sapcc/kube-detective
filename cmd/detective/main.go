package main

import (
	"flag"
	"fmt"
	"os"
	"os/signal"
	"syscall"

	"github.com/sapcc/kube-detective/pkg/detective"
	"k8s.io/klog"
)

var opts detective.Options

func init() {
	flag.StringVar(&opts.ExternalCIDR, "externalCIDR", "", "subnet used for external IPs")
	flag.StringVar(&opts.NodeFilterRegex, "nodeFilter", ".*", "filter node names with this regex")
	flag.BoolVar(&opts.TestPods, "pods", true, "test pods")
	flag.BoolVar(&opts.TestServices, "services", true, "test services")
	flag.BoolVar(&opts.TestExternalIPs, "externalips", false, "test external IPs")
}

func main() {
	flag.Set("alsologtostderr", "true")
	flag.Parse()

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
}
