package detective

import (
	"context"
	"fmt"
	"net"
	"os"
	"regexp"
	"time"

	"github.com/hashicorp/go-multierror"
	tomb "gopkg.in/tomb.v2"
	core "k8s.io/api/core/v1"
	"k8s.io/client-go/informers"
	"k8s.io/client-go/kubernetes"
	_ "k8s.io/client-go/plugin/pkg/client/auth/oidc"
	"k8s.io/client-go/rest"
	"k8s.io/client-go/tools/cache"
	"k8s.io/client-go/tools/clientcmd"
	"k8s.io/klog/v2"
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
	WorkerCount     int
	ExternalCIDR    string
	NodeFilterRegex string
	TestImage       string
	TestPods        bool
	TestServices    bool
	TestServiceName bool
	TestExternalIPs bool
	RestConfig      *rest.Config
}

type Detective struct {
	client    *kubernetes.Clientset
	config    *rest.Config
	informers informers.SharedInformerFactory

	namespace   *core.Namespace
	externalIPs []string
	nodeFilter  *regexp.Regexp
	workerCount int

	tomb      *tomb.Tomb
	outerTomb *tomb.Tomb
	testImage string
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

	d.testImage = opts.TestImage

	d.workerCount = opts.WorkerCount
	if d.workerCount < 1 {
		d.workerCount = 10 //default to 10 parallel checks
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

	if err := d.createClient(opts); err != nil {
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

	if opts.TestServices || opts.TestServiceName || opts.TestExternalIPs {
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
	var result *multierror.Error
	if opts.TestPods {
		fmt.Println("Pod --> Pod")
		result = multierror.Append(result, d.hitPods(false, false))

		fmt.Println("Pod (hostNetwork) --> Pod")
		result = multierror.Append(result, d.hitPods(true, false))

		fmt.Println("Pod  --> Pod (hostNetwork)")
		result = multierror.Append(result, d.hitPods(false, true))

		fmt.Println("Pod (hostNetwork) --> Pod (hostNetwork)")
		result = multierror.Append(result, d.hitPods(true, true))
	}

	if opts.TestServices {
		fmt.Println("Pod --> ClusterIP --> Pod")
		result = multierror.Append(result, d.hitServices(false, false))

		fmt.Println("Pod (hostNetwork) --> ClusterIP --> Pod")
		result = multierror.Append(result, d.hitServices(true, false))

		fmt.Println("Pod --> ClusterIP --> Pod (hostNetwork)")
		result = multierror.Append(result, d.hitServices(false, true))

		fmt.Println("Pod (hostNetwork) --> ClusterIP --> Pod (hostNetwork)")
		result = multierror.Append(result, d.hitServices(true, true))
	}

	if opts.TestServiceName {
		fmt.Println("Pod --> Service Name (ClusterIP) --> Pod")
		result = multierror.Append(result, d.hitServiceName())
	}

	if opts.TestExternalIPs {
		fmt.Println("Pod --> ExternalIP --> Pod")
		result = multierror.Append(result, d.hitExternalIP(false, false))

		fmt.Println("Pod (hostNetwork) --> ExternalIP --> Pod")
		result = multierror.Append(result, d.hitExternalIP(true, false))

		fmt.Println("Pod --> ExternalIP --> Pod (hostNetwork)")
		result = multierror.Append(result, d.hitExternalIP(false, true))

		fmt.Println("Pod (hostNetwork) --> ExternalIP --> Pod (hostNetwork)")
		result = multierror.Append(result, d.hitExternalIP(true, true))
	}

	return result.ErrorOrNil()
}

func (d *Detective) cleanup() error {
	klog.V(2).Infof("Cleaning Up")
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

func (d *Detective) createClient(opts Options) error {

	klog.V(2).Infof("Creating Client")
	config := opts.RestConfig
	if config == nil {
		rules := clientcmd.NewDefaultClientConfigLoadingRules()
		overrides := &clientcmd.ConfigOverrides{}
		var err error

		opts.RestConfig, err = clientcmd.NewNonInteractiveDeferredLoadingClientConfig(rules, overrides).ClientConfig()
		if err != nil {
			return err
		}
	}

	client, err := kubernetes.NewForConfig(config)
	if err != nil {
		return err
	}

	d.client = client
	d.config = config
	klog.V(3).Infof("  Host: %s", config.Host)
	klog.V(3).Infof("  User: %s", config.Username)
	klog.V(3).Infof("  Key:  %s", config.KeyFile)

	return nil
}

func (d *Detective) createInformers() {
	klog.V(2).Infof("Creating Informers")

	d.informers = informers.NewSharedInformerFactoryWithOptions(d.client, InformerResyncPeriod, informers.WithNamespace(d.namespace.Name))
	nodes := d.informers.Core().V1().Nodes().Informer()
	pods := d.informers.Core().V1().Pods().Informer()
	services := d.informers.Core().V1().Services().Informer()
	endpoints := d.informers.Core().V1().Endpoints().Informer()

	d.informers.Start(d.tomb.Dying())

	klog.V(2).Infof("Waiting for Caches")
	cache.WaitForCacheSync(d.tomb.Dying(), nodes.HasSynced, pods.HasSynced, services.HasSynced, endpoints.HasSynced)
}
