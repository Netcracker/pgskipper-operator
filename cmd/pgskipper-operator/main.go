// Copyright 2024-2025 NetCracker Technology Corporation
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
//     http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.

package main

import (
	"context"
	"flag"

	site "github.com/Netcracker/pgskipper-operator/pkg/disasterrecovery"
	"github.com/Netcracker/pgskipper-operator/pkg/helper"
	"github.com/Netcracker/pgskipper-operator/pkg/vault"
	"github.com/Netcracker/qubership-credential-manager/pkg/hook"

	"net/http"
	"os"
	"strings"

	qubershipv1 "github.com/Netcracker/pgskipper-operator/api/apps/v1"
	patroniv1 "github.com/Netcracker/pgskipper-operator/api/patroni/v1"
	"github.com/Netcracker/pgskipper-operator/pkg/util"

	// Import all Kubernetes client auth plugins (e.g. Azure, GCP, OIDC, etc.)
	// to ensure that exec-entrypoint and run can make use of them.
	_ "k8s.io/client-go/plugin/pkg/client/auth"

	"k8s.io/apimachinery/pkg/runtime"
	utilruntime "k8s.io/apimachinery/pkg/util/runtime"
	clientgoscheme "k8s.io/client-go/kubernetes/scheme"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/cache"
	"sigs.k8s.io/controller-runtime/pkg/healthz"
	"sigs.k8s.io/controller-runtime/pkg/log/zap"
	metricsserver "sigs.k8s.io/controller-runtime/pkg/metrics/server"

	"github.com/Netcracker/pgskipper-operator/controllers"
	"github.com/operator-framework/operator-lib/leader"
	//+kubebuilder:scaffold:imports
)

var (
	scheme   = runtime.NewScheme()
	setupLog = ctrl.Log.WithName("setup")
)

func init() {
	utilruntime.Must(clientgoscheme.AddToScheme(scheme))
	utilruntime.Must(patroniv1.AddToScheme(scheme))
	utilruntime.Must(qubershipv1.AddToScheme(scheme))
	//+kubebuilder:scaffold:scheme
}

func main() {
	var metricsAddr string
	var enableLeaderElection bool
	var probeAddr string
	operatorRole := strings.ToLower(os.Getenv("OPERATOR_ROLE"))
	flag.StringVar(&metricsAddr, "metrics-bind-address", ":8383", "The address the metric endpoint binds to.")
	flag.StringVar(&probeAddr, "health-probe-bind-address", ":8081", "The address the probe endpoint binds to.")
	flag.BoolVar(&enableLeaderElection, "leader-elect", false,
		"Enable leader election for controller manager. "+
			"Enabling this will ensure there is only one active controller manager.")
	opts := zap.Options{
		Development: true,
	}
	opts.BindFlags(flag.CommandLine)
	flag.Parse()

	ctrl.SetLogger(zap.New(zap.UseFlagOptions(&opts)))
	lockName := "postgres-operator-lock"
	if operatorRole == "patroni" {
		lockName = "patroni-core-operator-lock"
	}
	// Become the leader before proceeding
	err := leader.Become(context.TODO(), lockName)
	if err != nil {
		setupLog.Error(err, "")
		os.Exit(1)
	}

	mgr, err := ctrl.NewManager(ctrl.GetConfigOrDie(), ctrl.Options{
		Scheme:                 scheme,
		Metrics:                metricsserver.Options{BindAddress: "0"},
		HealthProbeBindAddress: probeAddr,
		LeaderElection:         enableLeaderElection,
		LeaderElectionID:       "3100713b.qubership.org",
		Cache: cache.Options{
			DefaultNamespaces: map[string]cache.Config{util.GetNameSpace(): {}},
		},
	})

	if err != nil {
		setupLog.Error(err, "unable to start manager")
		os.Exit(1)
	}
	if operatorRole == "patroni" {
		setupLog.Info("Creating new PatroniCore controller ")
		if err = controllers.NewPatroniCoreReconciler(mgr.GetClient(), mgr.GetScheme()).SetupWithManager(mgr); err != nil {
			setupLog.Error(err, "unable to create controller", "controller", "PatroniCore")
			os.Exit(1)
		}
	} else {
		setupLog.Info("Creating new PatroniServices controller ")
		if err = (controllers.NewPostgresServiceReconciler(mgr.GetClient(), mgr.GetScheme())).SetupWithManager(mgr); err != nil {
			setupLog.Error(err, "unable to create controller", "controller", "PatroniServices")
			os.Exit(1)

		}
		//Init section
		vault.Init()
		site.InitDRManager()
	}
	//+kubebuilder:scaffold:builder

	if err := mgr.AddHealthzCheck("healthz", healthz.Ping); err != nil {
		setupLog.Error(err, "unable to set up health check")
		os.Exit(1)
	}
	if err := mgr.AddReadyzCheck("readyz", healthz.Ping); err != nil {
		setupLog.Error(err, "unable to set up ready check")
		os.Exit(1)
	}

	go func() {
		var tlsEnabled = false
		if operatorRole != "patroni" {
			cr, _ := helper.GetHelper().GetPostgresServiceCR()
			if cr.Spec.Tls != nil && cr.Spec.Tls.Enabled {
				tlsEnabled = true
			}
		}
		var errServ error
		if tlsEnabled {
			errServ = http.ListenAndServeTLS(":8443", "/certs/tls.crt", "/certs/tls.key", nil)
		} else {
			errServ = http.ListenAndServe(":8080", nil)
		}
		if errServ != nil {
			setupLog.Error(errServ, "problem with operator server")
		}
	}()

	_ = hook.ClearHooks()
	setupLog.Info("Starting the Cmd.")

	setupLog.Info("starting manager")
	if err := mgr.Start(ctrl.SetupSignalHandler()); err != nil {
		setupLog.Error(err, "problem running manager")
		os.Exit(1)
	}
}
