package detective

import (
	"bytes"
	"io"
	"net/url"
	"strings"

	v1 "k8s.io/api/core/v1"
	"k8s.io/client-go/kubernetes/scheme"
	restclient "k8s.io/client-go/rest"
	"k8s.io/client-go/tools/remotecommand"
	"k8s.io/klog/v2"
)

// ExecOptions passed to ExecWithOptions
type ExecOptions struct {
	Command []string

	Namespace     string
	PodName       string
	ContainerName string

	Stdin         io.Reader
	CaptureStdout bool
	CaptureStderr bool
	// If false, whitespace in std{err,out} will be removed.
	PreserveWhitespace bool
}

// ExecWithOptions executes a command in the specified container,
// returning stdout, stderr and error. `options` allowed for
// additional parameters to be passed.
func (d *Detective) ExecWithOptions(options ExecOptions) (string, string, error) {
	const tty = false

	req := d.client.CoreV1().RESTClient().Post().
		Resource("pods").
		Name(options.PodName).
		Namespace(options.Namespace).
		SubResource("exec").
		VersionedParams(&v1.PodExecOptions{
			Container: options.ContainerName,
			Command:   options.Command,
			Stdin:     options.Stdin != nil,
			Stdout:    options.CaptureStdout,
			Stderr:    options.CaptureStderr,
			TTY:       tty,
		}, scheme.ParameterCodec)

	var stdout, stderr bytes.Buffer
	// Here be Dragons
	err := execute("POST", req.URL(), d.config, options.Stdin, &stdout, &stderr, tty)
	if err != nil {
		klog.V(3).Infof("Stream fail: %v", err)
	}

	if options.PreserveWhitespace {
		return stdout.String(), stderr.String(), err
	}
	return strings.TrimSpace(stdout.String()), strings.TrimSpace(stderr.String()), err
}

func execute(method string, url *url.URL, config *restclient.Config, stdin io.Reader, stdout, stderr io.Writer, tty bool) error {
	exec, err := remotecommand.NewSPDYExecutor(config, method, url)
	if err != nil {
		klog.V(3).Infof("Executor fail: %v", err)
		return err
	}
	return exec.Stream(remotecommand.StreamOptions{
		Stdin:  stdin,
		Stdout: stdout,
		Stderr: stderr,
		Tty:    tty,
	})
}
