// Command epp is the main entrypoint and composition root for the OSCAR
// InferenceMesh Endpoint Picker (EPP) and OSCAR Elastic Autoscaler skeleton.
package main

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"log"
	"os"
	"time"

	"github.com/grycap/oscar-inferencemesh/src/internal/application"
	"github.com/grycap/oscar-inferencemesh/src/internal/config"
	"github.com/grycap/oscar-inferencemesh/src/internal/domain"
	"github.com/grycap/oscar-inferencemesh/src/internal/infrastructure/extproc"
	"github.com/grycap/oscar-inferencemesh/src/internal/infrastructure/k8s"
	"github.com/grycap/oscar-inferencemesh/src/internal/infrastructure/metrics"
	"github.com/grycap/oscar-inferencemesh/src/internal/infrastructure/oscar"
)

func buildSelector(settings config.Settings) domain.EndpointSelectorPort {
	mode := domain.RoutingMode(settings.RoutingMode)
	if mode == domain.RoutingModePassThrough {
		return application.NewPassThroughSelector()
	}
	return application.NewActiveSelector(mode, settings.MetricsMaxAge)
}

func buildServer(settings config.Settings) *extproc.Server {
	discovery := k8s.NewEndpointDiscoveryAdapter(settings)
	metricsCollector := metrics.NewVllmPrometheusAdapter(settings)
	oscarClient := oscar.NewControlPlaneAdapter(settings)

	selector := buildSelector(settings)
	pickerService := application.NewEndpointPickerServiceWithWaitTimeout(
		discovery,
		metricsCollector,
		selector,
		settings.WorkerWaitTimeout,
	)

	// TODO(FG-05): Wire configured scaling thresholds and replica bounds into an active policy; keep no-op available.
	autoscalerPolicy := application.NewNoOpAutoscalerPolicy()
	autoscaler := application.NewAutoscalerOrchestrator(
		settings.ModelName,
		discovery,
		metricsCollector,
		autoscalerPolicy,
		oscarClient,
	)

	return extproc.NewServer(settings, pickerService, autoscaler)
}

func main() {
	selfCheck := flag.Bool(
		"self-check",
		false,
		"Run a one-shot pass-through routing and OSCAR autoscaler verification check, print JSON evidence, and exit.",
	)
	portOverride := flag.Int(
		"port",
		0,
		"Override the HTTP verification server port (default: 9091).",
	)
	flag.Parse()

	settings := config.FromEnv()
	if *portOverride > 0 {
		settings.HTTPPort = *portOverride
	}

	server := buildServer(settings)

	if *selfCheck {
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()

		simDecision := server.SimulateRequest(ctx, extproc.SimulateRequestPayload{
			RequestID: "self-check-001",
			Model:     settings.ModelName,
			Prompt:    "Verify Golang EPP pass-through pipeline and OSCAR connectivity",
		})
		report := server.BuildVerificationReport(ctx)
		report.SimulatedRoutingOutcome = &simDecision

		encoded, err := json.MarshalIndent(report, "", "  ")
		if err != nil {
			log.Fatalf("failed to serialize verification report: %v", err)
		}
		fmt.Println(string(encoded))
		os.Exit(0)
	}

	if err := server.ListenAndServe(); err != nil {
		log.Fatalf("[EPP] Server exited with error: %v", err)
	}
}
