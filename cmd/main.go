package main

import (
	"flag"
	"fmt"
	"log"
	"os"

	"github.com/serverscom/api-gateway-controller/internal/config"
	"github.com/serverscom/api-gateway-controller/internal/flags"
	"github.com/serverscom/api-gateway-controller/internal/gateway/controller"
	lbsrv "github.com/serverscom/api-gateway-controller/internal/service/lb"
	tlssrv "github.com/serverscom/api-gateway-controller/internal/service/tls"

	clientgoscheme "k8s.io/client-go/kubernetes/scheme"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/cache"
	"sigs.k8s.io/controller-runtime/pkg/healthz"
	ctrlZap "sigs.k8s.io/controller-runtime/pkg/log/zap"
	"sigs.k8s.io/controller-runtime/pkg/metrics/server"

	"k8s.io/apimachinery/pkg/runtime"
	gatewayv1 "sigs.k8s.io/gateway-api/apis/v1"
)

var (
	version   string
	gitCommit string
	scheme    = runtime.NewScheme()
	setupLog  = ctrl.Log.WithName("setup")
)

func init() {
	_ = clientgoscheme.AddToScheme(scheme)
	_ = gatewayv1.Install(scheme)
}

func main() {
	var opts ctrlZap.Options
	opts.BindFlags(flag.CommandLine)
	ctrlConf, err := flags.ParseFlags()
	if err != nil {
		log.Fatalf("Error parsing flags: %v\n", err)
	}

	if ctrlConf.ShowVersion {
		fmt.Printf("Version=%v GitCommit=%v\n", version, gitCommit)
		os.Exit(0)
	}

	ctrl.SetLogger(ctrlZap.New(ctrlZap.UseFlagOptions(&opts)))

	mgr, err := ctrl.NewManager(ctrl.GetConfigOrDie(), ctrl.Options{
		Scheme: scheme,
		Cache: cache.Options{
			DefaultNamespaces: map[string]cache.Config{
				ctrlConf.Namespace: {},
			},
		},
		Metrics: server.Options{
			BindAddress: ctrlConf.MetricsAddr,
		},
		HealthProbeBindAddress: ctrlConf.ProbeAddr,
		LeaderElection:         ctrlConf.EnableLeaderElection,
	})
	if err != nil {
		setupLog.Error(err, "unable to start manager")
		os.Exit(1)
	}

	// setup sc client
	scCli, err := config.NewServerscomClient()
	if err != nil {
		setupLog.Error(err, "unable to create servers.com client")
		os.Exit(1)
	}
	scCli.SetupUserAgent(fmt.Sprintf("%s/%s %s", ctrlConf.ControllerName, version, gitCommit))

	// setup gw class reconciler
	if err = (&controller.GatewayClassReconciler{
		Client:         mgr.GetClient(),
		ControllerName: ctrlConf.ControllerName,
	}).SetupWithManager(mgr); err != nil {
		setupLog.Error(err, "unable to create controller", "controller", "GatewayClass")
		os.Exit(1)
	}

	// setup gw reconciler
	if err = (&controller.GatewayReconciler{
		Client:           mgr.GetClient(),
		Recorder:         mgr.GetEventRecorderFor("gateway-controller"),
		ControllerName:   ctrlConf.ControllerName,
		GatewayClassName: ctrlConf.GatewayClassName,
		LBMgr:            lbsrv.NewManager(scCli),
		TLSMgr:           tlssrv.NewManager(scCli),
	}).SetupWithManager(mgr); err != nil {
		setupLog.Error(err, "unable to create controller", "controller", "Gateway")
		os.Exit(1)
	}

	// Health checks
	if err := mgr.AddHealthzCheck("healthz", healthz.Ping); err != nil {
		setupLog.Error(err, "unable to set up health check")
		os.Exit(1)
	}
	if err := mgr.AddReadyzCheck("readyz", healthz.Ping); err != nil {
		setupLog.Error(err, "unable to set up ready check")
		os.Exit(1)
	}

	setupLog.Info("starting manager", "version", version, "gitCommit", gitCommit)
	if err := mgr.Start(ctrl.SetupSignalHandler()); err != nil {
		setupLog.Error(err, "problem running manager")
		os.Exit(1)
	}

}
