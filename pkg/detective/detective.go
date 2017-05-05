package detective

import (
	"fmt"
	"net"
	"os"
	"time"

	"k8s.io/client-go/1.5/kubernetes"
	"k8s.io/client-go/1.5/pkg/api/v1"
)

var (
	VERSION = "0.0.0-dev"
)

const (
	ServiceAccountProvisionTimeout = 2 * time.Minute
	PodStartTimeout                = 1 * time.Minute
	WaitForEndpointInterval        = 5 * time.Second
	WaitForEndpointTimeout         = 1 * time.Minute

	PodHttpPort     = 9376
	ServiceHttpPort = 9377
)

type Options struct {
	ExternalCIDR string
}

type Detective struct {
	client      *kubernetes.Clientset
	namespace   *v1.Namespace
	nodes       []v1.Node
	pods        []*v1.Pod
	services    []*v1.Service
	externalIPs []string
}

func NewDetective(opts Options) *Detective {
	if opts.ExternalCIDR == "" {
		fmt.Println("You need to provide a flag -externalCIDR")
		os.Exit(1)
	}

	return &Detective{
		externalIPs: opts.externalIPs(),
	}
}

func (d *Detective) Run() {
	d.init()
	d.setup()
	d.execute()
	d.cleanup()

	defer func() {
		if err := recover(); err != nil {
			d.Stop()
		}
	}()
}

func (d *Detective) Stop() {
	d.cleanup()
	os.Exit(1)
}

func (d *Detective) init() {
	fmt.Printf("Welcome to Detective %v\n", VERSION)
	d.createClient()
}

func (d *Detective) setup() {
	d.getReadySchedulableNodes()
	d.createNamespace()
	d.waitForServiceAccountInNamespace()
	d.createPods()
	d.createServices()
}

func (d *Detective) execute() {
	fmt.Println("Pod --> Pod")
	d.hitPods(false, false)

	fmt.Println("Pod (hostNetwork) --> Pod")
	d.hitPods(true, false)

	fmt.Println("Pod  --> Pod (hostNetwork)")
	d.hitPods(false, true)

	fmt.Println("Pod (hostNetwork) --> Pod (hostNetwork)")
	d.hitPods(true, true)

	fmt.Println("Pod --> ClusterIP --> Pod")
	d.hitServices(false, false)

	fmt.Println("Pod (hostNetwork) --> ClusterIP --> Pod")
	d.hitServices(true, false)

	fmt.Println("Pod --> ClusterIP --> Pod (hostNetwork)")
	d.hitServices(false, true)

	fmt.Println("Pod (hostNetwork) --> ClusterIP --> Pod (hostNetwork)")
	d.hitServices(true, true)

	fmt.Println("Pod --> ExternalIP --> Pod")
	d.hitExternalIP(false, false)

	fmt.Println("Pod (hostNetwork) --> ExternalIP --> Pod")
	d.hitExternalIP(true, false)

	fmt.Println("Pod --> ExternalIP --> Pod (hostNetwork)")
	d.hitExternalIP(false, true)

	fmt.Println("Pod (hostNetwork) --> ExternalIP --> Pod (hostNetwork)")
	d.hitExternalIP(true, true)
}

func (d *Detective) cleanup() {
	d.deleteNamespace()
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
