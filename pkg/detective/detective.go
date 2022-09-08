package detective

import (
	"context"
	"github.com/go-logr/logr"
	"net"
	"os"
	"regexp"
	"time"

	tomb "gopkg.in/tomb.v2"
	core "k8s.io/api/core/v1"
	"k8s.io/client-go/informers"
	"k8s.io/client-go/kubernetes"
	_ "k8s.io/client-go/plugin/pkg/client/auth/oidc"
	"k8s.io/client-go/rest"
	"k8s.io/client-go/tools/cache"
	"k8s.io/client-go/tools/clientcmd"
)

var (
	VERSION = "0.0.0-dev"
)

const (
	PodStartTimeout         = 1 * time.Minute
	WaitForEndpointInterval = 5 * time.Second
	WaitForEndpointTimeout  = 1 * time.Minute
	InformerResyncPeriod    = 1 * time.Minute

	PodHttpPort     = 9376
	ServiceHttpPort = 9377
)

type Options struct {
	ExternalCIDR    string
	NodeFilterRegex string
	TestImage       string
	TestPods        bool
	TestServices    bool
	TestExternalIPs bool
}

type Detective struct {
	client    *kubernetes.Clientset
	config    *rest.Config
	informers informers.SharedInformerFactory

	namespace   *core.Namespace
	externalIPs []string
	nodeFilter  *regexp.Regexp

	tomb      *tomb.Tomb
	outerTomb *tomb.Tomb
	testImage string

	log logr.Logger
	opts Options
}

func NewDetective(opts Options, logger logr.Logger) *Detective {
	outerTomb, ctx := tomb.WithContext(context.Background())
	innerTomb, _ := tomb.WithContext(ctx)

	d := &Detective{
		tomb:      innerTomb,
		outerTomb: outerTomb,
		log:       logger,
		opts:      opts,
	}

	if opts.TestExternalIPs {
		var err error
		d.externalIPs, err = d.checkExternalIPs(opts)
		if err != nil {
			d.log.Error(err, "error checking external ips")
			os.Exit(1)
		}
	}

	e, err := regexp.Compile(opts.NodeFilterRegex)
	if err != nil {
		d.log.Error(err, "error compiling regex")
		os.Exit(1)
	} else {
		d.nodeFilter = e
	}

	d.testImage = opts.TestImage


	return d
}

func (d *Detective) Run() {
	d.tomb.Go(func() error {
		if err := d.setup(d.opts); err != nil {
			return err
		}

		if err := d.execute(d.opts); err != nil {
			return err
		}

		return nil
	})

	d.outerTomb.Go(func() error {
		<-d.tomb.Dead()
		if d.tomb.Err() != nil {
			d.cleanup()
			return d.tomb.Err()
		} else {
			return d.cleanup()
		}
	})
}

func (d *Detective) Kill() {
	d.outerTomb.Kill(nil)
}

func (d *Detective) Wait() error {
	return d.outerTomb.Wait()
}

func (d *Detective) setup(opts Options) error {
	//fmt.Printf("Welcome to Detective %v\n", VERSION)
	d.log.Info("welcome to detective", "version", VERSION)

	if err := d.createClient(); err != nil {
		return err
	}

	if err := d.createNamespace(); err != nil {
		return err
	}

	d.createInformers()

	if err := d.waitForServiceAccountInNamespace(); err != nil {
		return err
	}

	if err := d.createPods(); err != nil {
		return err
	}

	if err := d.waitForPodsRunning(); err != nil {
		return err
	}

	if opts.TestServices || opts.TestExternalIPs {
		if err := d.createSevices(opts.TestExternalIPs); err != nil {
			return err
		}

		if err := d.waitForServiceEndpoints(); err != nil {
			return err
		}
	}

	return nil
}

func (d *Detective) execute(opts Options) error {
	if opts.TestPods {
		d.log.Info("Pod --> Pod")
		if err := d.hitPods(false, false); err != nil {
			return err
		}

		d.log.Info("Pod (hostNetwork) --> Pod")
		if err := d.hitPods(true, false); err != nil {
			return err
		}

		d.log.Info("Pod  --> Pod (hostNetwork)")
		if err := d.hitPods(false, true); err != nil {
			return err
		}

		d.log.Info("Pod (hostNetwork) --> Pod (hostNetwork)")
		if err := d.hitPods(true, true); err != nil {
			return err
		}
	}

	if opts.TestServices {
		d.log.Info("Pod --> ClusterIP --> Pod")
		if err := d.hitServices(false, false); err != nil {
			return err
		}

		d.log.Info("Pod (hostNetwork) --> ClusterIP --> Pod")
		if err := d.hitServices(true, false); err != nil {
			return err
		}

		d.log.Info("Pod --> ClusterIP --> Pod (hostNetwork)")
		if err := d.hitServices(false, true); err != nil {
			return err
		}

		d.log.Info("Pod (hostNetwork) --> ClusterIP --> Pod (hostNetwork)")
		if err := d.hitServices(true, true); err != nil {
			return err
		}
	}

	if opts.TestExternalIPs {
		d.log.Info("Pod --> ExternalIP --> Pod")
		if err := d.hitExternalIP(false, false); err != nil {
			return err
		}

		d.log.Info("Pod (hostNetwork) --> ExternalIP --> Pod")
		if err := d.hitExternalIP(true, false); err != nil {
			return err
		}

		d.log.Info("Pod --> ExternalIP --> Pod (hostNetwork)")
		if err := d.hitExternalIP(false, true); err != nil {
			return err
		}

		d.log.Info("Pod (hostNetwork) --> ExternalIP --> Pod (hostNetwork)")
		if err := d.hitExternalIP(true, true); err != nil {
			return err
		}
	}

	return nil
}

func (d *Detective) cleanup() error {
	d.log.Info("Cleaning Up")
	if d.namespace != nil {
		return d.deleteNamespace()
	}
	return nil
}

func (d *Detective) checkExternalIPs(o Options) ([]string,error) {
	ip, ipnet, err := net.ParseCIDR(o.ExternalCIDR)
	if err != nil {
		d.log.Error(err, "error parsing externalCIDR")
		return nil, err
	}

	var ips []string
	for ip := ip.Mask(ipnet.Mask); ipnet.Contains(ip); inc(ip) {
		ips = append(ips, ip.String())
	}

	return ips, nil
}

func (d *Detective) createClient() error {
	//glog.V(2).Infof("Creating Client")
	d.log.Info("creating client")
	rules := clientcmd.NewDefaultClientConfigLoadingRules()
	overrides := &clientcmd.ConfigOverrides{}

	config, err := clientcmd.NewNonInteractiveDeferredLoadingClientConfig(rules, overrides).ClientConfig()
	if err != nil {
		return err
	}

	client, err := kubernetes.NewForConfig(config)
	if err != nil {
		return err
	}

	d.client = client
	d.config = config
	//glog.V(3).Infof("  Host: %s", config.Host)
	d.log.Info("", "host", config.Host)
	//glog.V(3).Infof("  User: %s", config.Username)
	d.log.Info("", "user", config.Username)
	//glog.V(3).Infof("  Key:  %s", config.KeyFile)
	d.log.Info("", "key", config.KeyFile)

	return nil
}

func (d *Detective) createInformers() {
	//glog.V(2).Infof("Creating Informers")
	d.log.Info("creating informers")

	d.informers = informers.NewSharedInformerFactoryWithOptions(d.client, InformerResyncPeriod, informers.WithNamespace(d.namespace.Name))
	nodes := d.informers.Core().V1().Nodes().Informer()
	pods := d.informers.Core().V1().Pods().Informer()
	services := d.informers.Core().V1().Services().Informer()
	endpoints := d.informers.Core().V1().Endpoints().Informer()

	d.informers.Start(d.tomb.Dying())

	//glog.V(2).Infof("Waiting for Caches")
	d.log.Info("waiting for caches")
	cache.WaitForCacheSync(d.tomb.Dying(), nodes.HasSynced, pods.HasSynced, services.HasSynced, endpoints.HasSynced)
}
