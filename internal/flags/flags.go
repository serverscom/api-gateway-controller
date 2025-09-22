package flags

import (
	"flag"
	"os"

	"github.com/serverscom/api-gateway-controller/internal/config"

	"github.com/spf13/pflag"
	v1 "k8s.io/api/core/v1"
)

type Configuration struct {
	ShowVersion bool

	Namespace string

	MetricsAddr          string
	ProbeAddr            string
	EnableLeaderElection bool

	GatewayClassName string
	ControllerName   string
	LBLabelSelector  string
}

func ParseFlags() (*Configuration, error) {
	var (
		flags = pflag.NewFlagSet("", pflag.ExitOnError)

		showVersion = flags.Bool("version", false,
			`Show controller version and exit.`)

		watchNamespace = flags.String("watch-namespace", v1.NamespaceAll,
			`Namespace to watch for Services/Endpoints. (Optional)`)

		metricsAddr = flags.String("metrics-bind-address", ":8080",
			"The address the metric endpoint binds to.")
		probeAddr = flags.String("health-probe-bind-address", ":8081",
			"The address the probe endpoint binds to.")
		enableLeaderElection = flags.Bool("leader-elect", false,
			"Enable leader election for controller manager.")
		gatewayClassName = flags.String("gateway-class-name", config.DEFAULT_GATEWAY_CLASS,
			`Name of the GatewayClass this controller watches. (Optional, empty = watch all)`)
		controllerName = flags.String("controller-name", config.DEFAULT_CONTROLLER_NAME,
			`Controller field to match in GatewayClass resources.`)
		lbLabelSelector = flags.String("lb-label-selector", config.GW_LABEL_ID,
			`Label selector key for Services representing API Gateways.`)
	)

	flags.AddGoFlagSet(flag.CommandLine)

	if err := flags.Parse(os.Args); err != nil {
		return nil, err
	}

	conf := &Configuration{
		ShowVersion: *showVersion,

		Namespace: *watchNamespace,

		MetricsAddr:          *metricsAddr,
		ProbeAddr:            *probeAddr,
		EnableLeaderElection: *enableLeaderElection,

		GatewayClassName: *gatewayClassName,
		ControllerName:   *controllerName,
		LBLabelSelector:  *lbLabelSelector,
	}

	return conf, nil
}
