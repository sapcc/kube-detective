package detective

import (
	"context"
	"fmt"
	"net"
	"os"
	"regexp"
	"time"

	"github.com/golang/glog"
	tomb "gopkg.in/tomb.v2"
	core "k8s.io/api/core/v1"
	"k8s.io/client-go/informers"
	"k8s.io/client-go/kubernetes"
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
}

func NewDetective(opts Options) *Detective {
	outerTomb, ctx := tomb.WithContext(context.Background())
	innerTomb, _ := tomb.WithContext(ctx)

	d := &Detective{
		tomb:      innerTomb,
		outerTomb: outerTomb,
	}

	if opts.TestExternalIPs {
		if opts.ExternalCIDR == "" {
			fmt.Println("You need to provide a flag -externalCIDR")
			os.Exit(1)
		}

		d.externalIPs = opts.externalIPs()
	}

	e, err := regexp.Compile(opts.NodeFilterRegex)
	if err != nil {
		fmt.Println("The -nodeFilter paramter is not a valid regex")
		os.Exit(1)
	} else {
		d.nodeFilter = e
	}

	d.tomb.Go(func() error {
		if err := d.setup(opts); err != nil {
			return err
		}

		if err := d.execute(opts); err != nil {
			return err
		}

		return nil
	})

	outerTomb.Go(func() error {
		<-innerTomb.Dead()
		if innerTomb.Err() != nil {
			d.cleanup()
			return innerTomb.Err()
		} else {
			return d.cleanup()
		}
	})

	return d
}

func (d *Detective) Kill() {
	d.outerTomb.Kill(nil)
}

func (d *Detective) Wait() error {
	return d.outerTomb.Wait()
}

func (d *Detective) setup(opts Options) error {
	fmt.Printf("Welcome to Detective %v\n", VERSION)

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
		fmt.Println("Pod --> Pod")
		if err := d.hitPods(false, false); err != nil {
			return err
		}

		fmt.Println("Pod (hostNetwork) --> Pod")
		if err := d.hitPods(true, false); err != nil {
			return err
		}

		fmt.Println("Pod  --> Pod (hostNetwork)")
		if err := d.hitPods(false, true); err != nil {
			return err
		}

		fmt.Println("Pod (hostNetwork) --> Pod (hostNetwork)")
		if err := d.hitPods(true, true); err != nil {
			return err
		}
	}

	if opts.TestServices {
		fmt.Println("Pod --> ClusterIP --> Pod")
		if err := d.hitServices(false, false); err != nil {
			return err
		}

		fmt.Println("Pod (hostNetwork) --> ClusterIP --> Pod")
		if err := d.hitServices(true, false); err != nil {
			return err
		}

		fmt.Println("Pod --> ClusterIP --> Pod (hostNetwork)")
		if err := d.hitServices(false, true); err != nil {
			return err
		}

		fmt.Println("Pod (hostNetwork) --> ClusterIP --> Pod (hostNetwork)")
		if err := d.hitServices(true, true); err != nil {
			return err
		}
	}

	if opts.TestExternalIPs {
		fmt.Println("Pod --> ExternalIP --> Pod")
		if err := d.hitExternalIP(false, false); err != nil {
			return err
		}

		fmt.Println("Pod (hostNetwork) --> ExternalIP --> Pod")
		if err := d.hitExternalIP(true, false); err != nil {
			return err
		}

		fmt.Println("Pod --> ExternalIP --> Pod (hostNetwork)")
		if err := d.hitExternalIP(false, true); err != nil {
			return err
		}

		fmt.Println("Pod (hostNetwork) --> ExternalIP --> Pod (hostNetwork)")
		if err := d.hitExternalIP(true, true); err != nil {
			return err
		}
	}

	return nil
}

func (d *Detective) cleanup() error {
	glog.V(2).Infof("Cleaning Up")
	if d.namespace != nil {
		return d.deleteNamespace()
	}
	return nil
}

func (o *Options) externalIPs() []string {
	ip, ipnet, err := net.ParseCIDR(o.ExternalCIDR)
	if err != nil {
		fmt.Printf("Couldn't parse externalCIDR: %v\n", err)
		os.Exit(1)
	}

	var ips []string
	for ip := ip.Mask(ipnet.Mask); ipnet.Contains(ip); inc(ip) {
		ips = append(ips, ip.String())
	}

	return ips
}

func (d *Detective) createClient() error {
	glog.V(2).Infof("Creating Client")
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
	glog.V(3).Infof("  Host: %s", config.Host)
	glog.V(3).Infof("  User: %s", config.Username)
	glog.V(3).Infof("  Key:  %s", config.KeyFile)

	return nil
}

func (d *Detective) createInformers() {
	glog.V(2).Infof("Creating Informers")

	d.informers = informers.NewSharedInformerFactoryWithOptions(d.client, InformerResyncPeriod, informers.WithNamespace(d.namespace.Name))
	nodes := d.informers.Core().V1().Nodes().Informer()
	pods := d.informers.Core().V1().Pods().Informer()
	services := d.informers.Core().V1().Services().Informer()
	endpoints := d.informers.Core().V1().Endpoints().Informer()

	d.informers.Start(d.tomb.Dying())

	glog.V(2).Infof("Waiting for Caches")
	cache.WaitForCacheSync(d.tomb.Dying(), nodes.HasSynced, pods.HasSynced, services.HasSynced, endpoints.HasSynced)
}
