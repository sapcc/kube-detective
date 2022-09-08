package cmd

import (
	"context"
	"github.com/go-logr/logr"
	"github.com/go-logr/zapr"
	"github.com/prometheus/client_golang/prometheus/promhttp"
	"github.com/robfig/cron/v3"
	"github.com/sapcc/kube-detective/pkg/detective"
	"github.com/spf13/cobra"
	"github.com/spf13/viper"
	"go.uber.org/zap"
	"net/http"
	"os"
	"os/signal"
	"syscall"
)

var RootCmd = &cobra.Command{
	Use: "kube-detective",
	RunE: rootCmdRunE,
}

var opts detective.Options


func init() {
	viper.AutomaticEnv()
	viper.SetEnvPrefix("DETECTIVE")
	RootCmd.Flags().StringVar(&opts.ExternalCIDR, "externalCIDR", "", "subnet used for external IPs")
	RootCmd.Flags().StringVar(&opts.NodeFilterRegex, "nodeFilter", ".*", "filter node names with this regex")
	RootCmd.Flags().BoolVar(&opts.TestPods, "pods", true, "test pods")
	RootCmd.Flags().BoolVar(&opts.TestServices, "services", true, "test services")
	RootCmd.Flags().BoolVar(&opts.TestExternalIPs, "externalips", false, "test external IPs")
	RootCmd.Flags().StringVar(&opts.TestImage, "test-image", "gcr.io/google_containers/serve_hostname:1.2", "test external IPs")
	RootCmd.MarkFlagsRequiredTogether("externalips", "externalCIDR")
	_ = viper.BindPFlags(RootCmd.Flags())
}

/*
func main() {

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
*/

func rootCmdRunE (cmd *cobra.Command, args []string) error {

	var log logr.Logger
	zapLog, err := zap.NewDevelopment()
	if err != nil {
		return err
	}
	log = zapr.NewLogger(zapLog).WithName("root")

	log.Info("Starting up")
	cr := cron.New(cron.WithChain(cron.SkipIfStillRunning(log)))

	log.Info("creating detective")
	det := detective.NewDetective(opts, log.WithName("detective"))

	_, err = cr.AddJob("* * * * *", det)

	if err != nil {
		log.Error(err, "error adding func to cron")
		return err
	}

	cr.Start()

	server := &http.Server{Addr: ":8080", Handler: promhttp.Handler()}
	go func() {
		if err := server.ListenAndServe(); err != nil {
			log.Error(err, "error listening to web server")
			return
		}
	}()
	stop := make(chan os.Signal, 1)
	signal.Notify(stop, os.Interrupt, syscall.SIGTERM)

	<-stop
	log.Info("Shutting down ...")
	cr.Stop()
	err = server.Shutdown(context.Background())
	if err != nil {
		log.Error(err, "error shutting down")
		return err
	}
	return nil
}