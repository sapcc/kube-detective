package detective

import (
	"context"
	"fmt"
	"github.com/go-logr/logr"
	"os"
	"os/signal"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"
	"syscall"
	"time"
)

type Reconciler struct {
	Log    logr.Logger
	Client client.Client
	Cfg    Options
}

func (r *Reconciler) Reconcile(ctx context.Context, request reconcile.Request) (reconcile.Result, error) {
	log := r.Log.WithValues("node", request.Name)

	sigs := make(chan os.Signal, 1)
	signal.Notify(sigs, os.Interrupt, syscall.SIGTERM)

	d := NewDetective(r.Cfg)

	go func() {
		<-sigs
		d.Kill()
	}()

	err := d.Wait()
	if err != nil {
		log.Error(err, "error running detective")
		os.Exit(1)
	}
	var timer time.Duration = 5 * time.Minute

	fmt.Print("controller has finished\n")
	return reconcile.Result{Requeue: true, RequeueAfter: timer}, nil
}
