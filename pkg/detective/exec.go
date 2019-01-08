package detective

import (
	"bytes"
	"fmt"
	"io"
	"net/url"
	"strings"

	"github.com/golang/glog"
	"k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes/scheme"
	restclient "k8s.io/client-go/rest"
	"k8s.io/client-go/tools/remotecommand"
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

	req := d.client.RESTClient().Post().
		Resource("pods").
		Name(options.PodName).
		Namespace(options.Namespace).
		SubResource("exec").
		Param("container", options.ContainerName)
	req.VersionedParams(&v1.PodExecOptions{
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
		glog.V(3).Infof("Stream fail: %v", err)
	}

	if options.PreserveWhitespace {
		return stdout.String(), stderr.String(), err
	}
	return strings.TrimSpace(stdout.String()), strings.TrimSpace(stderr.String()), err
}

// ExecCommandInContainerWithFullOutput executes a command in the
// specified container and return stdout, stderr and error
func (d *Detective) ExecCommandInContainerWithFullOutput(namespace, podName, containerName string, cmd ...string) (string, string, error) {
	return d.ExecWithOptions(ExecOptions{
		Command:       cmd,
		Namespace:     namespace,
		PodName:       podName,
		ContainerName: containerName,

		Stdin:              nil,
		CaptureStdout:      true,
		CaptureStderr:      true,
		PreserveWhitespace: false,
	})
}

// ExecCommandInContainer executes a command in the specified container.
func (d *Detective) ExecCommandInContainer(namespace, podName, containerName string, cmd ...string) string {
	stdout, _, err := d.ExecCommandInContainerWithFullOutput(namespace, podName, containerName, cmd...)
	if err != nil {
		return fmt.Sprintf("failed to execute command in pod %v, container %v: %v", podName, containerName, err)
	}
	return stdout
}

func (d *Detective) ExecShellInContainer(namespace, podName, containerName string, cmd string) string {
	return d.ExecCommandInContainer(namespace, podName, containerName, "/bin/sh", "-c", cmd)
}

func (d *Detective) ExecCommandInPod(namespace, podName string, cmd ...string) string {
	pod, err := d.client.CoreV1().Pods(namespace).Get(podName, metav1.GetOptions{})
	if err != nil {
		return fmt.Sprintf("failed to get pod %s/%s: %s", namespace, podName, err)
	}
	return d.ExecCommandInContainer(namespace, podName, pod.Spec.Containers[0].Name, cmd...)
}

func (d *Detective) ExecCommandInPodWithFullOutput(namespace, podName string, cmd ...string) (string, string, error) {
	pod, err := d.client.CoreV1().Pods(namespace).Get(podName, metav1.GetOptions{})
	if err != nil {
		return "", "", fmt.Errorf("failed to get pod")
	}
	return d.ExecCommandInContainerWithFullOutput(namespace, podName, pod.Spec.Containers[0].Name, cmd...)
}

func (d *Detective) ExecShellInPod(namespace, podName string, cmd string) string {
	return d.ExecCommandInPod(namespace, podName, "/bin/sh", "-c", cmd)
}

func (d *Detective) ExecShellInPodWithFullOutput(namespace, podName string, cmd string) (string, string, error) {
	return d.ExecCommandInPodWithFullOutput(namespace, podName, "/bin/sh", "-c", cmd)
}

func execute(method string, url *url.URL, config *restclient.Config, stdin io.Reader, stdout, stderr io.Writer, tty bool) error {
	exec, err := remotecommand.NewSPDYExecutor(config, method, url)
	if err != nil {
		glog.V(3).Infof("Executor fail: %v", err)
		return err
	}
	return exec.Stream(remotecommand.StreamOptions{
		Stdin:  stdin,
		Stdout: stdout,
		Stderr: stderr,
		Tty:    tty,
	})
}
