package main

import (
	"flag"
	"os"
	"os/signal"
	"syscall"

	"github.com/sapcc/kube-detective/pkg/detective"
)

var opts detective.Options

func init() {
	flag.StringVar(&opts.ExternalCIDR, "externalCIDR", "", "subnet used for external IPs")
}

func main() {
	flag.Parse()

	sigs := make(chan os.Signal, 1)
	done := make(chan bool, 1)
	signal.Notify(sigs, os.Interrupt, syscall.SIGTERM)

	d := detective.NewDetective(opts)

	go func() {
		d.Run()
		done <- true
	}()

	go func() {
		<-sigs
		d.Stop()
		done <- true
	}()

	<-done
}
