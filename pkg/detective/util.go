package detective

import (
	"bytes"
	"context"
	"fmt"
	"net"
	"os/exec"
	"strings"
	"time"

	"github.com/golang/glog"
	core "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/watch"
)

func (d *Detective) NodeIsSchedulabeleAndRunning(node *core.Node) bool {
	if node.Spec.Unschedulable {
		return false
	}

	if len(node.Status.Conditions) == 0 {
		return false
	}

	if !d.nodeFilter.MatchString(node.Name) {
		return false
	}

	for _, cond := range node.Status.Conditions {
		if cond.Type == core.NodeReady && cond.Status != core.ConditionTrue {
			glog.V(3).Infof("Ignoring node %v with %v condition status %v", node.Name, cond.Type, cond.Status)
			return false
		}
	}
	return true
}

func ServiceAccountHasSecret(event watch.Event) (bool, error) {
	switch event.Type {
	case watch.Deleted:
		return false, errors.NewNotFound(schema.GroupResource{Resource: "serviceaccounts"}, "")
	}
	switch t := event.Object.(type) {
	case *core.ServiceAccount:
		return len(t.Secrets) > 0, nil
	}
	return false, nil
}

func inc(ip net.IP) {
	for j := len(ip) - 1; j >= 0; j-- {
		ip[j]++
		if ip[j] > 0 {
			break
		}
	}
}

func RunHostCmd(ctx context.Context, ns, name, cmd string) (string, error) {
	return RunKubectl(ctx, "exec", fmt.Sprintf("--namespace=%v", ns), name, "--", "/bin/sh", "-c", cmd)
}

func RunKubectl(ctx context.Context, args ...string) (string, error) {
	return NewKubectlCommand(ctx, args...).Exec()
}

func NewKubectlCommand(ctx context.Context, args ...string) *kubectlBuilder {
	b := new(kubectlBuilder)
	b.cmd = KubectlCmd(ctx, args...)
	return b
}

func KubectlCmd(ctx context.Context, args ...string) *exec.Cmd {
	cmd := exec.CommandContext(ctx, "kubectl", args...)
	return cmd
}

type kubectlBuilder struct {
	cmd     *exec.Cmd
	timeout <-chan time.Time
}

func (b kubectlBuilder) Exec() (string, error) {
	var stdout, stderr bytes.Buffer
	cmd := b.cmd
	cmd.Stdout, cmd.Stderr = &stdout, &stderr

	glog.V(4).Infof("Running '%s %s'", cmd.Path, strings.Join(cmd.Args[1:], " ")) // skip arg[0] as it is printed separately
	if err := cmd.Start(); err != nil {
		return "", fmt.Errorf("Error starting %v:\nCommand stdout:\n%v\nstderr:\n%v\nerror:\n%v\n", cmd, cmd.Stdout, cmd.Stderr, err)
	}
	errCh := make(chan error, 1)
	go func() {
		errCh <- cmd.Wait()
	}()
	select {
	case err := <-errCh:
		if err != nil {
			return "", fmt.Errorf("Error running %v:\nCommand stdout:\n%v\nstderr:\n%v\nerror:\n%v\n", cmd, cmd.Stdout, cmd.Stderr, err)
		}
	case <-b.timeout:
		b.cmd.Process.Kill()
		return "", fmt.Errorf("Timed out waiting for command %v:\nCommand stdout:\n%v\nstderr:\n%v\n", cmd, cmd.Stdout, cmd.Stderr)
	}
	glog.V(4).Infof("stderr: %q", stderr.String())
	return stdout.String(), nil
}
