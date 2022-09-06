package cmd

import (
	"fmt"
	"github.com/sapcc/kube-detective/internal/detective"
	metr "github.com/sapcc/kube-detective/internal/metrics"
	"github.com/spf13/cobra"
	"github.com/spf13/viper"
	corev1 "k8s.io/api/core/v1"
	_ "k8s.io/client-go/plugin/pkg/client/auth/oidc"
	"os"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client/config"
	"sigs.k8s.io/controller-runtime/pkg/controller"
	"sigs.k8s.io/controller-runtime/pkg/handler"
	"sigs.k8s.io/controller-runtime/pkg/log/zap"
	"sigs.k8s.io/controller-runtime/pkg/manager"
	"sigs.k8s.io/controller-runtime/pkg/manager/signals"
	"sigs.k8s.io/controller-runtime/pkg/metrics"
	"sigs.k8s.io/controller-runtime/pkg/source"
	"strconv"
)

var rootCmd = &cobra.Command{
	Use:   "kube-detective",
	Short: "",
	Long:  "",
	RunE:  runRootCmd,
}
var cfg = detective.Options{}

func init() {
	viper.AutomaticEnv()
	viper.SetEnvPrefix("DETECTIVE")
	rootCmd.PersistentFlags().StringVar(&cfg.ExternalCIDR, "externalCIDR", "", "subnet used for external IPs")
	rootCmd.PersistentFlags().StringVar(&cfg.NodeFilterRegex, "nodeFilter", ".*", "filter node names with this regex")
	rootCmd.PersistentFlags().BoolVar(&cfg.TestPods, "pods", true, "test pods")
	rootCmd.PersistentFlags().BoolVar(&cfg.TestServices, "services", true, "test services")
	rootCmd.PersistentFlags().BoolVar(&cfg.TestExternalIPs, "externalips", false, "test external IPs")
	rootCmd.PersistentFlags().StringVar(&cfg.TestImage, "test-image", "gcr.io/google_containers/serve_hostname:1.2", "test external IPs")
	rootCmd.PersistentFlags().IntVar(&cfg.MetricsPort, "metrics_port", 30042, "Port for Prometheus metrics")
	rootCmd.PersistentFlags().IntVar(&cfg.HealthPort, "health_port", 30043, "Port for healthz")
	_ = viper.BindPFlags(rootCmd.PersistentFlags())

	metrics.Registry.MustRegister(
		metr.TestTotal, metr.ErrorTotal, metr.PodIPTest, metr.PodIPTestError,
		metr.ClusterIPTest, metr.ClusterIPTestError, metr.ExternalIPTest, metr.ExternalIPTestError)
}

func runRootCmd(cmd *cobra.Command, args []string) error {
	log := zap.New(func(o *zap.Options) {
		o.Development = true
	}).WithName("runRoot")
	ctrl.SetLogger(log)

	managerOpts := manager.Options{
		MetricsBindAddress:     ":" + strconv.Itoa(cfg.MetricsPort),
		HealthProbeBindAddress: ":" + strconv.Itoa(cfg.HealthPort),
	}

	restConfig, err := config.GetConfigWithContext(cfg.KubeContext)
	if err != nil {
		log.Error(err, "error getting kube config. Exiting.")
		os.Exit(1)
	}
	mgr, err := manager.New(restConfig, managerOpts)
	if err != nil {
		log.Error(err, "error creating manager. Exiting.")
		os.Exit(1)
	}

	// add kube-detective
	c, err := controller.New("kube-detective", mgr, controller.Options{
		Reconciler: &detective.Reconciler{
			Log:    mgr.GetLogger().WithName("kube-detective"),
			Client: mgr.GetClient(),
			Cfg:    cfg,
		},
	})
	if err != nil {
		log.Error(err, "error creating node-controller")
		return err
	}
	err = c.Watch(&source.Kind{Type: &corev1.Node{}}, &handler.EnqueueRequestForObject{})
	if err != nil {
		log.Error(err, "error watching nodes")
		return err
	}

	/*
		// add detective-controller
		det := controller.Controller{
			Log: log.WithName("detective-controller"),
			Cfg: cfg,
		}

		err = mgr.Add(&det)
		if err != nil {
			log.Error(err, "error adding detective-controller")
			return err
		}

	*/
	err = mgr.Start(signals.SetupSignalHandler())
	if err != nil {
		log.Error(err, "error starting manager")
		return err
	}
	return nil
}

func Execute() {
	if err := rootCmd.Execute(); err != nil {
		_, _ = fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}
