// Package main implements the oscar-im CLI tool for OSCAR InferenceMesh.
package main

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/grycap/oscar-inferencemesh/src/internal/config"
	"github.com/grycap/oscar-inferencemesh/src/internal/infrastructure/k8s"
	"github.com/grycap/oscar-inferencemesh/src/internal/infrastructure/oscar"
)

const (
	cliVersion = "0.1.0"
	banner     = `
   ____  _____ _____ ___     ____          ________  ___
  / __ \/ ___// ____//   |   / __ \        /  _/   |/  /
 / / / /\__ \/ /    / /| |  / /_/ / _____  / / / /|_/ / 
/ /_/ /___/ / /___ / ___ | / _, _/ /____/ / / / /  / /  
\____//____/\____//_/  |_|/_/ |_|       /___//_/  /_/   
OSCAR-IM v` + cliVersion + "\n"
)

type initOptions struct {
	docker             bool
	skipGateway        bool
	keepGateway        bool
	force              bool
	oscarEndpoint      string
	oscarCluster       string
	modelName          string
	namespace          string
	gatewayName        string
	gatewayPort        int
	gatewayServiceType string
	httpPort           int
	grpcPort           int
	workerWaitTimeout  int
}

type removeOptions struct {
	keepGateway   bool
	skipGateway   bool
	namespace     string
	modelName     string
	gatewayName   string
	docker        bool
	force         bool
	oscarEndpoint string
	oscarCluster  string
	httpPort      int
	grpcPort      int
}

func main() {
	if len(os.Args) < 2 {
		printUsage()
		os.Exit(1)
	}

	command := os.Args[1]
	switch command {
	case "init":
		handleInit(os.Args[2:], false)
	case "model":
		handleModel(os.Args[2:])
	case "status", "info", "check":
		handleStatus(os.Args[2:])
	case "remove":
		handleRemove(os.Args[2:], false)
	case "restart":
		handleRestart(os.Args[2:])
	case "config":
		handleConfig(os.Args[2:])
	case "version", "--version", "-v":
		fmt.Printf("oscar-im CLI version %s\n", cliVersion)
	case "help", "--help", "-h":
		printUsage()
	default:
		fmt.Fprintf(os.Stderr, "Unknown command: %s\n\n", command)
		printUsage()
		os.Exit(1)
	}
}

func printUsage() {
	fmt.Print(banner)
	fmt.Println("Usage: oscar-im <command> [options]")
	fmt.Println()
	fmt.Println("Available Commands:")
	fmt.Println("  init       Initialize Envoy AI Gateway (controllers + data plane), EPP, and ext_proc filter")
	fmt.Println("  model      Manage models connected to the Envoy Gateway (add, remove, list)")
	fmt.Println("  status     Inspect deployed mesh, connected models, OSCAR/K8s pods, Gateway, and EPP (aliases: info, check)")
	fmt.Println("  remove     Stop and remove deployed components (EPP, EnvoyExtensionPolicy, Gateways)")
	fmt.Println("  restart    Restart the mesh infrastructure (remove followed by init)")
	fmt.Println("  config     View and edit persistent configuration (~/.config/oscar-im/config.yaml)")
	fmt.Println("  version    Display the CLI version")
	fmt.Println("  help       Show help information")
	fmt.Println()
	fmt.Println("Run 'oscar-im <command> --help' for details on specific command options.")
}

func handleStatus(args []string) {
	fs := flag.NewFlagSet("status", flag.ExitOnError)

	fileCfg, _ := config.LoadOrCreateFileConfig("")
	defaultEndpoint := "https://localhost.direct"
	defaultCluster := "local"
	defaultModel := "qwen2-5-05b-instruct"
	defaultNamespace := "oscar-svc"
	defaultHTTPPort := 9091

	if fileCfg != nil {
		if fileCfg.Defaults.Model != "" {
			defaultModel = fileCfg.Defaults.Model
		}
		if fileCfg.Defaults.Namespace != "" {
			defaultNamespace = fileCfg.Defaults.Namespace
		}
		if fileCfg.Defaults.HTTPPort > 0 {
			defaultHTTPPort = fileCfg.Defaults.HTTPPort
		}
		if clusterName, cl, ok := fileCfg.GetActiveCluster(); ok {
			defaultCluster = clusterName
			if cl.Endpoint != "" {
				defaultEndpoint = cl.Endpoint
			}
		}
	}

	opts := k8s.StatusOptions{
		ExtensionPolicyName: "envoy-ai-gateway-epp-ext-proc",
		EPPServiceName:      "oscar-inferencemesh-epp",
	}
	var jsonOut bool
	fs.StringVar(&opts.Namespace, "namespace", envOrDefault("EPP_NAMESPACE", defaultNamespace), "Target Kubernetes namespace")
	fs.StringVar(&opts.ModelName, "model", envOrDefault("EPP_MODEL_NAME", defaultModel), "Target LLM model name")
	fs.StringVar(&opts.GatewayName, "gateway", "envoy-ai-gateway", "Target Envoy Gateway resource name")
	fs.StringVar(&opts.OscarEndpoint, "oscar-endpoint", envOrDefault("OSCAR_ENDPOINT", defaultEndpoint), "OSCAR REST endpoint URL")
	fs.StringVar(&opts.OscarClusterID, "oscar-cluster", envOrDefault("OSCAR_CLUSTER_ID", defaultCluster), "OSCAR cluster ID")
	fs.IntVar(&opts.EPPHTTPPort, "http-port", defaultHTTPPort, "EPP HTTP verification port")
	fs.IntVar(&opts.GatewayPort, "gateway-port", 8080, "Local Envoy Gateway ingress port")
	fs.BoolVar(&jsonOut, "json", false, "Print full diagnostic report as JSON")
	_ = fs.Parse(args)

	settings := config.Settings{
		OscarEndpoint:      opts.OscarEndpoint,
		OscarClusterID:     opts.OscarClusterID,
		ModelName:          opts.ModelName,
		Namespace:          opts.Namespace,
		DefaultBackendPort: 8000,
	}
	oscarClient := oscar.NewControlPlaneAdapter(settings)
	discovery := k8s.NewEndpointDiscoveryAdapter(settings)
	inspector := k8s.NewDefaultMeshInspector(oscarClient, discovery)

	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()

	report := inspector.Inspect(ctx, opts)
	if jsonOut {
		enc := json.NewEncoder(os.Stdout)
		enc.SetIndent("", "  ")
		_ = enc.Encode(report)
		return
	}

	printStatusReport(report)
}

func printStatusReport(r k8s.MeshStatusReport) {
	fmt.Print(banner)
	fmt.Printf("===> Mesh Status Summary (namespace=%s, model=%s)\n\n", r.Namespace, r.ModelName)

	logStep("1. Kubernetes & OSCAR Control Plane")
	if r.KubeConnected {
		fmt.Printf("  - Kubernetes:        Connected (context=%s)\n", r.KubeContext)
	} else {
		fmt.Printf("  - Kubernetes:        UNREACHABLE (%s)\n", r.KubeError)
	}
	if r.Oscar.Connected {
		svcs := "none"
		if len(r.Oscar.RegisteredServices) > 0 {
			svcs = strings.Join(r.Oscar.RegisteredServices, ", ")
		}
		fmt.Printf("  - OSCAR Control:     Connected (cluster=%s, endpoint=%s, services=%d [%s])\n",
			r.Oscar.ClusterID, r.Oscar.Endpoint, len(r.Oscar.RegisteredServices), svcs)
		fmt.Printf("  - Target in OSCAR:   %v\n", r.Oscar.TargetRegistered)
	} else {
		fmt.Printf("  - OSCAR Control:     Disconnected (%s: %s)\n", r.Oscar.AdapterMode, r.Oscar.Error)
	}

	logStep("2. Model Pods & Endpoint Discovery")
	if r.Workload.LLMServiceExists {
		fmt.Printf("  - LLMInferenceSvc:   Present (desired replicas=%d)\n", r.Workload.DesiredReplicas)
	} else {
		fmt.Printf("  - LLMInferenceSvc:   Not found in namespace '%s'\n", r.Namespace)
	}
	fmt.Printf("  - Worker Pods:       %d Ready / %d Total\n", r.Workload.ReadyWorkerCount, r.Workload.TotalWorkerCount)
	for _, p := range r.Workload.WorkerPods {
		extra := ""
		if p.Issue != "" {
			extra = fmt.Sprintf(" [issue=%s]", p.Issue)
		}
		fmt.Printf("      * %-48s phase=%-11s ready=%s ip=%-13s restarts=%d%s\n",
			p.Name, p.Phase, p.Ready, p.PodIP, p.Restarts, extra)
	}
	if r.Workload.RouterScheduler != nil {
		rs := r.Workload.RouterScheduler
		fmt.Printf("  - Router Scheduler:  %s (phase=%s, ready=%s, ip=%s)\n", rs.Name, rs.Phase, rs.Ready, rs.PodIP)
	}
	fmt.Printf("  - EndpointSlice IPs: %d (%s)\n", len(r.Workload.EndpointSliceIPs), strings.Join(r.Workload.EndpointSliceIPs, ", "))
	fmt.Printf("  - EPP K8s Discovery: %d ready endpoint(s) detected for '%s'\n", len(r.Workload.DiscoveredEndpoints), r.ModelName)
	for _, ep := range r.Workload.DiscoveredEndpoints {
		fmt.Printf("      * %s -> %s:%d\n", ep.Name, ep.Address, ep.Port)
	}

	logStep("3. Envoy Gateway, Routes & ExtensionPolicy")
	fmt.Printf("  - Envoy Controller:  %s\n", r.Gateway.EnvoyControllerStatus)
	fmt.Printf("  - AI GW Controller:  %s\n", r.Gateway.AIGatewayControllerStatus)
	fmt.Printf("  - Data Plane Proxy:  %s\n", emptyFallback(r.Gateway.DataPlaneStatus, "Not provisioned"))
	fmt.Printf("  - Target Gateway:    %s/%s present=%v\n", r.Namespace, r.GatewayName, r.Gateway.TargetGatewayExists)
	if len(r.Gateway.Gateways) > 0 {
		for _, gw := range r.Gateway.Gateways {
			fmt.Printf("      * %s/%s (class=%s, programmed=%s)\n", gw.Namespace, gw.Name, gw.ClassName, gw.Programmed)
		}
	}
	nodePortStr := r.Gateway.GatewayNodePort30880
	if nodePortStr == "" {
		nodePortStr = "NONE (no Service binds nodePort 30880)"
	}
	fmt.Printf("  - NodePort 30880:    %s\n", nodePortStr)
	fmt.Printf("  - Localhost Port:    reachable=%v\n", r.Gateway.LocalGatewayPortOpen)
	fmt.Printf("  - AIGatewayRoutes:   %d in namespace '%s'\n", len(r.Gateway.AIGatewayRoutes), r.Namespace)
	for _, rt := range r.Gateway.AIGatewayRoutes {
		fmt.Printf("      * %s/%s (models=[%s], accepted=%s)\n", rt.Namespace, rt.Name, strings.Join(rt.Models, ", "), rt.Accepted)
	}
	ep := r.Gateway.ExtensionPolicy
	if ep.Exists {
		fmt.Printf("  - ExtensionPolicy:   %s -> %s/%s (targetExists=%v, accepted=%s, backend=%s, failOpen=%v)\n",
			ep.Name, ep.TargetKind, ep.TargetName, ep.TargetExists, ep.Accepted, ep.BackendRef, ep.FailOpen)
	} else {
		fmt.Printf("  - ExtensionPolicy:   Not found (%s)\n", ep.Name)
	}

	logStep("4. Connected Models")
	if len(r.ConnectedModels) == 0 {
		fmt.Printf("  - None connected via AIGatewayRoute in namespace '%s'.\n", r.Namespace)
		fmt.Printf("    Run 'oscar-im model add <model-name>' to expose a deployed model.\n")
	} else {
		for _, cm := range r.ConnectedModels {
			eppBadge := ""
			if cm.IsEPPDefaultTarget {
				eppBadge = " [EPP target]"
			}
			fmt.Printf("  - %s%s\n", cm.ModelName, eppBadge)
			fmt.Printf("      Route:       %s (accepted=%s, gateways=[%s])\n",
				cm.RouteName, emptyFallback(cm.RouteAccepted, "Unknown"), strings.Join(cm.GatewayRefs, ", "))
			fmt.Printf("      Backend:     %s\n", emptyFallback(cm.BackendTarget, "unresolved"))
			fmt.Printf("      OSCAR / K8s: oscarRegistered=%v, desiredReplicas=%d, workers=%d/%d Ready, endpointsDetected=%d\n",
				cm.RegisteredInOSCAR, cm.DesiredReplicas, cm.ReadyWorkers, cm.TotalWorkers, len(cm.DiscoveredEndpoints))
			for _, p := range cm.WorkerPods {
				fmt.Printf("        * pod=%-46s phase=%-10s ready=%s ip=%s\n", p.Name, p.Phase, p.Ready, p.PodIP)
			}
		}
	}
	if len(r.UnconnectedModels) > 0 {
		fmt.Printf("  - Unconnected OSCAR/KServe Models: %s\n", strings.Join(r.UnconnectedModels, ", "))
		fmt.Printf("    Hint: Run 'oscar-im model add <model-name>' to connect them to the Gateway.\n")
	}

	logStep("5. Endpoint Picker (EPP)")
	fmt.Printf("  - Deployment:        %s (pod=%s, phase=%s, ip=%s)\n",
		r.EPP.DeploymentStatus, r.EPP.PodName, r.EPP.PodPhase, r.EPP.PodIP)
	if r.EPP.VerifyReachable && r.EPP.Verify != nil {
		v := r.EPP.Verify
		fmt.Printf("  - /verify Source:    %s\n", r.EPP.VerifySource)
		fmt.Printf("  - Routing Mode:      %s (status=%s)\n", v.Mode, v.Status)
		fmt.Printf("  - Processed Reqs:    %d (recent decisions buffered=%d)\n",
			v.Picker.TotalRequestsProcessed, v.Picker.RecentDecisionsBuffered)
		fmt.Printf("  - EPP->OSCAR State:  connected=%v (mode=%s)\n",
			v.Autoscaler.OscarCluster.Connected, v.Autoscaler.OscarCluster.AdapterMode)
		fmt.Printf("  - Autoscaler Obs:    %s\n", v.Autoscaler.LastScalingDecision.Reason)
	} else {
		fmt.Printf("  - /verify State:     Unreachable (%s)\n", r.EPP.VerifyError)
	}

	logStep("6. Diagnostic Findings")
	for _, f := range r.Findings {
		switch f.Severity {
		case k8s.SeverityOK:
			fmt.Printf("  \033[0;32m[OK]\033[0m   [%s] %s\n", f.Component, f.Message)
		case k8s.SeverityWarn:
			fmt.Printf("  \033[1;33m[WARN]\033[0m [%s] %s\n", f.Component, f.Message)
		case k8s.SeverityFail:
			fmt.Printf("  \033[0;31m[FAIL]\033[0m [%s] %s\n", f.Component, f.Message)
		}
		if f.Hint != "" {
			fmt.Printf("         Hint: %s\n", f.Hint)
		}
	}
	fmt.Println()
}

func handleConfig(args []string) {
	if err := config.RunConfigCommand(args, os.Stdin, os.Stdout); err != nil {
		logErr(err.Error())
		os.Exit(1)
	}
}

func handleInit(args []string, isRestart bool) {
	fs := flag.NewFlagSet("init", flag.ExitOnError)

	fileCfg, _ := config.LoadOrCreateFileConfig("")
	defaultEndpoint := "https://localhost.direct"
	defaultCluster := "local"
	defaultModel := "qwen2-5-05b-instruct"
	defaultNamespace := "oscar-svc"
	defaultHTTPPort := 9091
	defaultGRPCPort := 9002
	defaultWorkerWaitTimeout := 30
	defaultDocker := false

	if fileCfg != nil {
		if fileCfg.Defaults.Model != "" {
			defaultModel = fileCfg.Defaults.Model
		}
		if fileCfg.Defaults.Namespace != "" {
			defaultNamespace = fileCfg.Defaults.Namespace
		}
		if fileCfg.Defaults.HTTPPort > 0 {
			defaultHTTPPort = fileCfg.Defaults.HTTPPort
		}
		if fileCfg.Defaults.GRPCPort > 0 {
			defaultGRPCPort = fileCfg.Defaults.GRPCPort
		}
		if fileCfg.Defaults.WorkerWaitTimeout > 0 {
			defaultWorkerWaitTimeout = fileCfg.Defaults.WorkerWaitTimeout
		}
		defaultDocker = fileCfg.Defaults.Docker
		if clusterName, cl, ok := fileCfg.GetActiveCluster(); ok {
			defaultCluster = clusterName
			if cl.Endpoint != "" {
				defaultEndpoint = cl.Endpoint
			}
		}
	}

	opts := initOptions{}
	fs.BoolVar(&opts.docker, "docker", defaultDocker, "Deploy EPP inside Kubernetes/Docker container instead of a local host process")
	fs.BoolVar(&opts.skipGateway, "skip-gateway", false, "Skip Envoy AI Gateway deployment check (default: false)")
	fs.BoolVar(&opts.keepGateway, "keep-gateway", false, "Alias for --skip-gateway (keep Envoy Gateway intact)")
	fs.BoolVar(&opts.force, "force", false, "Force rebuild and redeployment even if components are already running and available")
	fs.StringVar(&opts.oscarEndpoint, "oscar-endpoint", envOrDefault("OSCAR_ENDPOINT", defaultEndpoint), "OSCAR REST endpoint URL")
	fs.StringVar(&opts.oscarCluster, "oscar-cluster", envOrDefault("OSCAR_CLUSTER_ID", defaultCluster), "OSCAR cluster ID in configuration")
	fs.StringVar(&opts.modelName, "model", envOrDefault("EPP_MODEL_NAME", defaultModel), "Target LLM model name registered in OSCAR")
	fs.StringVar(&opts.namespace, "namespace", envOrDefault("EPP_NAMESPACE", defaultNamespace), "Target Kubernetes namespace")
	fs.StringVar(&opts.gatewayName, "gateway", "envoy-ai-gateway", "Envoy Gateway resource name")
	fs.IntVar(&opts.gatewayPort, "gateway-port", 8080, "Envoy Gateway HTTP ingress port")
	fs.StringVar(&opts.gatewayServiceType, "gateway-service-type", envOrDefault("GATEWAY_SERVICE_TYPE", "NodePort"), "Envoy proxy Service type (NodePort or LoadBalancer)")
	fs.IntVar(&opts.httpPort, "http-port", defaultHTTPPort, "EPP HTTP verification server port")
	fs.IntVar(&opts.grpcPort, "grpc-port", defaultGRPCPort, "EPP gRPC ext_proc server port")
	fs.IntVar(&opts.workerWaitTimeout, "worker-wait-timeout", defaultWorkerWaitTimeout, "Maximum seconds to wait for a ready worker (default: 30)")

	_ = fs.Parse(args)
	if opts.workerWaitTimeout <= 0 {
		logErr("--worker-wait-timeout must be greater than zero")
		os.Exit(1)
	}
	if fileCfg != nil {
		if err := persistWorkerWaitTimeout(fileCfg, opts.workerWaitTimeout); err != nil {
			logErr(fmt.Sprintf("failed to persist worker wait timeout: %v", err))
			os.Exit(1)
		}
	}
	if opts.keepGateway {
		opts.skipGateway = true
	}

	if !isRestart {
		fmt.Print(banner)
	}
	fmt.Println("===> Initializing OSCAR-IM...")
	fmt.Printf("  - Deployment Mode:    %s\n", deploymentModeString(opts.docker))
	fmt.Printf("  - Model Target:       %s\n", opts.modelName)
	fmt.Printf("  - Namespace:          %s\n", opts.namespace)
	fmt.Printf("  - OSCAR Endpoint:     %s (Cluster: %s)\n", opts.oscarEndpoint, opts.oscarCluster)
	fmt.Printf("  - ExtProc gRPC Port:  %d\n", opts.grpcPort)
	fmt.Printf("  - Worker Wait Timeout: %d seconds\n", opts.workerWaitTimeout)
	fmt.Printf("  - HTTP Verify Port:   %d\n", opts.httpPort)
	fmt.Println()

	repoRoot := findRepoRoot()
	deployer := k8s.NewDefaultKubeDeployer()

	// 1. Pre-flight check: Kubernetes cluster connectivity with auto-retry
	logStep("1. Verifying Kubernetes Cluster connectivity")
	k8sChecker := k8s.NewDefaultClusterChecker()
	k8sCtx, cancelK8s := context.WithTimeout(context.Background(), 45*time.Second)
	defer cancelK8s()

	k8sStatus := k8sChecker.CheckReadiness(k8sCtx, 15, 2*time.Second, func(attempt, max int, wait time.Duration) {
		logWarn(fmt.Sprintf("Kubernetes API server not responding yet (attempt %d/%d). Waiting %v...", attempt, max, wait))
	})
	if !k8sStatus.Ready {
		logErr(k8sStatus.Error.Error())
		os.Exit(1)
	}
	logInfo(fmt.Sprintf("Connected to Kubernetes: %s", k8sStatus.EndpointInfo))

	// 2. Validate OSCAR connectivity
	logStep("2. Validating OSCAR Control Plane connectivity")
	settings := config.Settings{
		OscarEndpoint:  opts.oscarEndpoint,
		OscarClusterID: opts.oscarCluster,
		ModelName:      opts.modelName,
		Namespace:      opts.namespace,
	}
	oscarClient := oscar.NewControlPlaneAdapter(settings)
	oscarCtx, cancelOscar := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancelOscar()

	clusterInfo := oscarClient.GetClusterInfo(oscarCtx)
	if clusterInfo.Connected {
		logInfo(fmt.Sprintf("Connected to OSCAR cluster '%s' at %s", opts.oscarCluster, opts.oscarEndpoint))
		svcSummary, _ := oscarClient.GetService(oscarCtx, opts.modelName)
		if svcSummary != nil {
			logInfo(fmt.Sprintf("Found registered service '%s' in OSCAR.", opts.modelName))
		} else {
			logWarn(fmt.Sprintf("Service '%s' is not yet registered in OSCAR (can be registered later).", opts.modelName))
		}
	} else {
		logWarn(fmt.Sprintf("OSCAR check note: %s (proceeding with gateway deployment...)", clusterInfo.Error))
	}

	deployCtx, cancelDeploy := context.WithTimeout(context.Background(), 5*time.Minute)
	defer cancelDeploy()

	// 3. Deploy/Verify Envoy AI Gateway (controllers + data plane)
	if !opts.skipGateway {
		logStep("3. Ensuring Envoy Gateway & Envoy AI Gateway are deployed")
		gatewayScript := filepath.Join(repoRoot, "scripts", "production", "deploy-gateway.sh")
		if fileExists(gatewayScript) {
			env := map[string]string{
				"FORCE": strconv.FormatBool(opts.force),
			}
			if err := runScriptWithEnv(gatewayScript, env); err != nil {
				logErr(fmt.Sprintf("Failed to deploy Envoy AI Gateway: %v", err))
				os.Exit(1)
			}
		} else {
			logInfo("Running in standalone binary mode (using embedded deployment orchestrator)...")
			if err := deployer.DeployGateway(deployCtx, opts.force); err != nil {
				logErr(fmt.Sprintf("Failed to deploy Envoy AI Gateway: %v", err))
				os.Exit(1)
			}
			logInfo("Envoy Gateway & Envoy AI Gateway controllers are deployed and Available.")
		}

		logInfo(fmt.Sprintf("Provisioning Envoy Gateway data plane '%s/%s' (port=%d, type=%s)...",
			opts.namespace, opts.gatewayName, opts.gatewayPort, opts.gatewayServiceType))
		gwOpts := k8s.GatewayDataPlaneOptions{
			GatewayName:        opts.gatewayName,
			GatewayNamespace:   opts.namespace,
			GatewayPort:        opts.gatewayPort,
			GatewayServiceType: opts.gatewayServiceType,
			Force:              opts.force,
		}
		if err := deployer.DeployGatewayDataPlane(deployCtx, gwOpts); err != nil {
			logErr(fmt.Sprintf("Failed to provision Envoy Gateway data plane: %v", err))
			os.Exit(1)
		}
		logInfo("Envoy Gateway data plane is Available and listening.")
	} else {
		logInfo("Skipping Gateway deployment as requested (--skip-gateway).")
	}

	// 4. Deploy EPP based on selected mode (Docker container vs Host direct process)
	if opts.docker {
		logStep("4. Deploying EPP in Container Mode (Kubernetes Pod)")
		deployEppScript := filepath.Join(repoRoot, "scripts", "production", "deploy-epp.sh")
		if fileExists(deployEppScript) {
			env := map[string]string{
				"NAMESPACE":                   opts.namespace,
				"MODEL_NAME":                  opts.modelName,
				"OSCAR_ENDPOINT":              opts.oscarEndpoint,
				"OSCAR_CLUSTER_ID":            opts.oscarCluster,
				"FORCE":                       strconv.FormatBool(opts.force),
				"EPP_WORKER_WAIT_TIMEOUT_SEC": strconv.Itoa(opts.workerWaitTimeout),
			}
			if err := runScriptWithEnv(deployEppScript, env); err != nil {
				logErr(fmt.Sprintf("Failed to deploy EPP container: %v", err))
				os.Exit(1)
			}
		} else {
			logInfo("Running in standalone binary mode (using embedded EPP deployment manifest)...")
			eppOpts := k8s.EPPDeployOptions{
				Namespace:         opts.namespace,
				ModelName:         opts.modelName,
				EPPImage:          envOrDefault("EPP_IMAGE", "oscar-inferencemesh-epp:latest"),
				OscarEndpoint:     opts.oscarEndpoint,
				OscarClusterID:    opts.oscarCluster,
				OscarVerifySSL:    envOrDefault("OSCAR_VERIFY_SSL", "false"),
				WorkerWaitTimeout: strconv.Itoa(opts.workerWaitTimeout),
				Force:             opts.force,
			}
			if err := deployer.DeployEPP(deployCtx, eppOpts); err != nil {
				logErr(fmt.Sprintf("Failed to deploy EPP container: %v", err))
				os.Exit(1)
			}
			logInfo("EPP is successfully deployed and running in namespace '" + opts.namespace + "'.")
		}

		// 5. Attach EnvoyExtensionPolicy (ext_proc gRPC filter)
		logStep("5. Attaching EnvoyExtensionPolicy (ext_proc gRPC)")
		extPolicyScript := filepath.Join(repoRoot, "scripts", "production", "deploy-extension-policy.sh")
		if fileExists(extPolicyScript) {
			policyEnv := map[string]string{
				"NAMESPACE": opts.namespace,
				"FORCE":     strconv.FormatBool(opts.force),
			}
			if err := runScriptWithEnv(extPolicyScript, policyEnv); err != nil {
				logErr(fmt.Sprintf("Failed to configure EnvoyExtensionPolicy: %v", err))
				os.Exit(1)
			}
		} else {
			logInfo("Running in standalone binary mode (using embedded EnvoyExtensionPolicy manifest)...")
			policyOpts := k8s.ExtensionPolicyOptions{
				Namespace:           opts.namespace,
				GatewayName:         "envoy-ai-gateway",
				EPPServiceName:      "oscar-inferencemesh-epp",
				EPPGRPCPort:         opts.grpcPort,
				ExtensionPolicyName: "envoy-ai-gateway-epp-ext-proc",
				Force:               opts.force,
			}
			if err := deployer.DeployExtensionPolicy(deployCtx, policyOpts); err != nil {
				logErr(fmt.Sprintf("Failed to configure EnvoyExtensionPolicy: %v", err))
				os.Exit(1)
			}
			logInfo("EnvoyExtensionPolicy successfully configured.")
		}
	} else {
		logStep("4. Configuring Host Execution Mode for EPP")
		logInfo("The EPP runs directly on the host machine.")
		logInfo(fmt.Sprintf("Listen Addresses: gRPC ext_proc on :%d, HTTP verification on :%d", opts.grpcPort, opts.httpPort))
		logInfo("To launch the EPP process on the host machine, run:")
		fmt.Printf("\n   go -C src run ./cmd/epp --port %d\n\n", opts.httpPort)

		// Also configure EnvoyExtensionPolicy so the Gateway is ready for the EPP
		logStep("5. Ensuring EnvoyExtensionPolicy CRD is applied")
		extPolicyScript := filepath.Join(repoRoot, "scripts", "production", "deploy-extension-policy.sh")
		if fileExists(extPolicyScript) {
			env := map[string]string{
				"NAMESPACE":  opts.namespace,
				"MODEL_NAME": opts.modelName,
				"FORCE":      strconv.FormatBool(opts.force),
			}
			_ = runScriptWithEnv(extPolicyScript, env)
		} else {
			policyOpts := k8s.ExtensionPolicyOptions{
				Namespace:           opts.namespace,
				GatewayName:         "envoy-ai-gateway",
				EPPServiceName:      "oscar-inferencemesh-epp",
				EPPGRPCPort:         opts.grpcPort,
				ExtensionPolicyName: "envoy-ai-gateway-epp-ext-proc",
				Force:               opts.force,
			}
			_ = deployer.DeployExtensionPolicy(deployCtx, policyOpts)
		}
	}

	logStep("Initialization Completed Successfully")
	fmt.Println("Summary:")
	fmt.Printf("  ✓ Envoy AI Gateway (%s/%s:%d) is ready\n", opts.namespace, opts.gatewayName, opts.gatewayPort)
	fmt.Printf("  ✓ EnvoyExtensionPolicy ext_proc on port %d is active\n", opts.grpcPort)
	if opts.docker {
		fmt.Printf("  ✓ EPP is deployed as a Kubernetes Service in namespace '%s'\n", opts.namespace)
	} else {
		fmt.Printf("  ✓ EPP is configured for host execution\n")
	}
	fmt.Printf("  → Next: deploy a model in OSCAR and connect its route manually:\n")
	fmt.Printf("      oscar-im model add %s --namespace %s\n\n", opts.modelName, opts.namespace)
}

func persistWorkerWaitTimeout(fileCfg *config.FileConfig, seconds int) error {
	if seconds <= 0 {
		return fmt.Errorf("worker wait timeout must be greater than zero")
	}
	fileCfg.Defaults.WorkerWaitTimeout = seconds
	return config.SaveFileConfig("", fileCfg)
}

func handleRestart(args []string) {
	fmt.Print(banner)
	fmt.Println("===> Restarting OSCAR-IM (remove + init)...")
	fmt.Println()

	handleRemove(args, true)
	handleInit(args, true)
}

func handleRemove(args []string, isRestart bool) {
	fs := flag.NewFlagSet("remove", flag.ExitOnError)

	fileCfg, _ := config.LoadOrCreateFileConfig("")
	defaultNamespace := "oscar-svc"
	defaultModel := "qwen2-5-05b-instruct"

	if fileCfg != nil {
		if fileCfg.Defaults.Namespace != "" {
			defaultNamespace = fileCfg.Defaults.Namespace
		}
		if fileCfg.Defaults.Model != "" {
			defaultModel = fileCfg.Defaults.Model
		}
	}

	opts := removeOptions{}
	fs.BoolVar(&opts.keepGateway, "keep-gateway", false, "Keep Envoy Gateway and Envoy AI Gateway intact; only remove EPP and ExtensionPolicy")
	fs.BoolVar(&opts.skipGateway, "skip-gateway", false, "Alias for --keep-gateway")
	fs.StringVar(&opts.namespace, "namespace", envOrDefault("EPP_NAMESPACE", defaultNamespace), "Target Kubernetes namespace")
	fs.StringVar(&opts.modelName, "model", envOrDefault("EPP_MODEL_NAME", defaultModel), "Target LLM model name")
	fs.StringVar(&opts.gatewayName, "gateway", "envoy-ai-gateway", "Envoy Gateway resource name")

	// Passthrough flags from init / restart
	fs.BoolVar(&opts.docker, "docker", false, "Ignored by remove (passed to init during restart)")
	fs.BoolVar(&opts.force, "force", false, "Ignored by remove (passed to init during restart)")
	fs.StringVar(&opts.oscarEndpoint, "oscar-endpoint", "", "Ignored by remove")
	fs.StringVar(&opts.oscarCluster, "oscar-cluster", "", "Ignored by remove")
	fs.IntVar(&opts.httpPort, "http-port", 9091, "Ignored by remove")
	fs.IntVar(&opts.grpcPort, "grpc-port", 9002, "Ignored by remove")
	fs.Int("worker-wait-timeout", 30, "Ignored by remove (passed to init during restart)")
	fs.Int("gateway-port", 8080, "Ignored by remove")
	fs.String("gateway-service-type", "NodePort", "Ignored by remove")

	_ = fs.Parse(args)
	if opts.skipGateway {
		opts.keepGateway = true
	}

	if !isRestart {
		fmt.Print(banner)
	}
	fmt.Println("===> Removing OSCAR-IM...")
	fmt.Printf("  - Namespace:    %s\n", opts.namespace)
	fmt.Printf("  - Keep Gateway: %v\n", opts.keepGateway)
	fmt.Println()

	repoRoot := findRepoRoot()
	deployer := k8s.NewDefaultKubeDeployer()

	// 1. Pre-flight check: Kubernetes cluster connectivity with auto-retry
	logStep("1. Verifying Kubernetes Cluster connectivity")
	k8sChecker := k8s.NewDefaultClusterChecker()
	k8sCtx, cancelK8s := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancelK8s()

	k8sStatus := k8sChecker.CheckReadiness(k8sCtx, 5, 2*time.Second, func(attempt, max int, wait time.Duration) {
		logWarn(fmt.Sprintf("Kubernetes API server not responding yet (attempt %d/%d). Waiting %v...", attempt, max, wait))
	})
	if !k8sStatus.Ready {
		logErr(k8sStatus.Error.Error())
		os.Exit(1)
	}
	logInfo(fmt.Sprintf("Connected to Kubernetes: %s", k8sStatus.EndpointInfo))

	cleanupCtx, cancelCleanup := context.WithTimeout(context.Background(), 3*time.Minute)
	defer cancelCleanup()

	// 2. Remove EnvoyExtensionPolicy (ext_proc gRPC filter)
	logStep("2. Removing EnvoyExtensionPolicy (ext_proc gRPC filter)")
	extPolicyScript := filepath.Join(repoRoot, "scripts", "production", "deploy-extension-policy.sh")
	if fileExists(extPolicyScript) {
		policyEnv := map[string]string{
			"NAMESPACE": opts.namespace,
		}
		if err := runScriptWithEnv(extPolicyScript, policyEnv, "--delete"); err != nil {
			logWarn(fmt.Sprintf("Note during EnvoyExtensionPolicy script removal: %v", err))
		}
	} else {
		logInfo("Running in standalone binary mode (removing EnvoyExtensionPolicy)...")
		policyOpts := k8s.ExtensionPolicyOptions{
			Namespace:           opts.namespace,
			ExtensionPolicyName: "envoy-ai-gateway-epp-ext-proc",
		}
		if err := deployer.RemoveExtensionPolicy(cleanupCtx, policyOpts); err != nil {
			logWarn(fmt.Sprintf("Note during EnvoyExtensionPolicy removal: %v", err))
		}
	}
	logInfo("EnvoyExtensionPolicy removed.")

	// 3. Remove EPP Deployment & Service
	logStep("3. Removing EPP (Deployment, Service, RBAC)")
	deployEppScript := filepath.Join(repoRoot, "scripts", "production", "deploy-epp.sh")
	if fileExists(deployEppScript) {
		env := map[string]string{
			"NAMESPACE": opts.namespace,
		}
		if err := runScriptWithEnv(deployEppScript, env, "--delete"); err != nil {
			logWarn(fmt.Sprintf("Note during EPP script removal: %v", err))
		}
	} else {
		logInfo("Running in standalone binary mode (removing EPP resources)...")
		eppOpts := k8s.EPPDeployOptions{
			Namespace: opts.namespace,
		}
		if err := deployer.RemoveEPP(cleanupCtx, eppOpts); err != nil {
			logWarn(fmt.Sprintf("Note during EPP removal: %v", err))
		}
	}
	logInfo("EPP resources removed.")

	// 4. Remove Gateways (if !keepGateway)
	if !opts.keepGateway {
		logStep("4. Removing model routes, Gateway data plane & Envoy controllers")
		_ = exec.CommandContext(cleanupCtx, "kubectl", "delete",
			"aigatewayroute,aiservicebackend,backend", "-n", opts.namespace,
			"-l", "app.kubernetes.io/part-of=oscar-inferencemesh", "--ignore-not-found=true").Run()
		if err := deployer.RemoveGatewayDataPlane(cleanupCtx, k8s.GatewayDataPlaneOptions{
			GatewayName:      opts.gatewayName,
			GatewayNamespace: opts.namespace,
		}); err != nil {
			logWarn(fmt.Sprintf("Note during Gateway data plane removal: %v", err))
		}

		gatewayScript := filepath.Join(repoRoot, "scripts", "production", "deploy-gateway.sh")
		if fileExists(gatewayScript) {
			if err := runScriptWithEnv(gatewayScript, nil, "--uninstall"); err != nil {
				logWarn(fmt.Sprintf("Note during Gateway script uninstallation: %v", err))
			}
		} else {
			logInfo("Running in standalone binary mode (uninstalling Envoy Gateways)...")
			if err := deployer.RemoveGateway(cleanupCtx); err != nil {
				logWarn(fmt.Sprintf("Note during Gateway removal: %v", err))
			}
		}
		logInfo("Envoy Gateway and Envoy AI Gateway uninstalled.")
	} else {
		logInfo("Keeping Envoy Gateway and Envoy AI Gateway intact (--keep-gateway).")
	}

	logStep("Removal Completed Successfully")
	fmt.Println("Summary:")
	fmt.Printf("  ✓ EnvoyExtensionPolicy removed\n")
	fmt.Printf("  ✓ EPP resources removed from namespace '%s'\n", opts.namespace)
	if !opts.keepGateway {
		fmt.Printf("  ✓ Envoy AI Gateway and Envoy Gateway uninstalled\n\n")
	} else {
		fmt.Printf("  ✓ Envoy Gateway retained\n\n")
	}
}

func deploymentModeString(docker bool) string {
	if docker {
		return "Docker Container / Kubernetes Pod (--docker)"
	}
	return "Direct Host Process (default)"
}

func runScript(scriptPath string) error {
	return runScriptWithEnv(scriptPath, nil)
}

func runScriptWithEnv(scriptPath string, extraEnv map[string]string, extraArgs ...string) error {
	cmdArgs := append([]string{"bash", scriptPath}, extraArgs...)
	cmd := exec.Command("/usr/bin/env", cmdArgs...)
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	cmd.Env = os.Environ()
	for k, v := range extraEnv {
		cmd.Env = append(cmd.Env, fmt.Sprintf("%s=%s", k, v))
	}
	return cmd.Run()
}

func fileExists(path string) bool {
	if path == "" {
		return false
	}
	info, err := os.Stat(path)
	return err == nil && !info.IsDir()
}

func findRepoRoot() string {
	// Start from current directory and walk upwards
	dir, err := os.Getwd()
	if err != nil {
		return ""
	}
	for {
		if _, err := os.Stat(filepath.Join(dir, "src", "go.mod")); err == nil {
			return dir
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			break
		}
		dir = parent
	}
	return ""
}

func envOrDefault(key, fallback string) string {
	if val := os.Getenv(key); val != "" {
		return val
	}
	return fallback
}

func logInfo(msg string) {
	fmt.Printf("\033[0;32m[INFO]\033[0m %s\n", msg)
}

func logWarn(msg string) {
	fmt.Printf("\033[1;33m[WARN]\033[0m %s\n", msg)
}

func logErr(msg string) {
	fmt.Printf("\033[0;31m[ERROR]\033[0m %s\n", msg)
}

func logStep(msg string) {
	fmt.Printf("\n\033[0;36m\033[1m===> %s\033[0m\n", msg)
}

func emptyFallback(val, fallback string) string {
	if strings.TrimSpace(val) == "" {
		return fallback
	}
	return val
}

func handleModel(args []string) {
	if len(args) == 0 || args[0] == "--help" || args[0] == "-h" || args[0] == "help" {
		printModelUsage()
		return
	}

	sub := args[0]
	rest := args[1:]
	switch sub {
	case "add", "connect":
		handleModelConnect(rest)
	case "remove", "disconnect", "rm":
		handleModelDisconnect(rest)
	case "list", "ls":
		handleModelList(rest)
	default:
		logErr(fmt.Sprintf("Unknown model subcommand: %s", sub))
		printModelUsage()
		os.Exit(1)
	}
}

func printModelUsage() {
	fmt.Println("Usage: oscar-im model <subcommand> [model-name] [options]")
	fmt.Println()
	fmt.Println("Subcommands:")
	fmt.Println("  add      Connect an existing OSCAR/KServe model to the Envoy Gateway (alias: connect)")
	fmt.Println("  remove   Disconnect a model's Envoy Gateway route and backends (aliases: disconnect, rm)")
	fmt.Println("  list     List connected models and unconnected OSCAR/KServe models (alias: ls)")
}

func loadModelDefaults() (namespace, model, endpoint, cluster string) {
	namespace = "oscar-svc"
	model = "qwen2-5-05b-instruct"
	endpoint = "https://localhost.direct"
	cluster = "local"
	if fileCfg, _ := config.LoadOrCreateFileConfig(""); fileCfg != nil {
		if fileCfg.Defaults.Namespace != "" {
			namespace = fileCfg.Defaults.Namespace
		}
		if fileCfg.Defaults.Model != "" {
			model = fileCfg.Defaults.Model
		}
		if clName, cl, ok := fileCfg.GetActiveCluster(); ok {
			cluster = clName
			if cl.Endpoint != "" {
				endpoint = cl.Endpoint
			}
		}
	}
	return namespace, model, endpoint, cluster
}

func reorderFlagArgs(args []string) ([]string, []string) {
	var flags, positional []string
	for i := 0; i < len(args); i++ {
		arg := args[i]
		if strings.HasPrefix(arg, "-") {
			flags = append(flags, arg)
			if !strings.Contains(arg, "=") && arg != "--force" && arg != "-force" && arg != "--help" && arg != "-h" {
				if i+1 < len(args) && !strings.HasPrefix(args[i+1], "-") {
					flags = append(flags, args[i+1])
					i++
				}
			}
		} else {
			positional = append(positional, arg)
		}
	}
	return flags, positional
}

func handleModelConnect(args []string) {
	defaultNS, defaultModel, defaultEndpoint, defaultCluster := loadModelDefaults()
	fs := flag.NewFlagSet("model add", flag.ExitOnError)
	opts := k8s.ModelRouteOptions{}
	var oscarEndpoint, oscarCluster string
	fs.StringVar(&opts.Namespace, "namespace", envOrDefault("EPP_NAMESPACE", defaultNS), "Target Kubernetes namespace")
	fs.StringVar(&opts.ModelName, "model", "", "Model name registered in OSCAR/KServe")
	fs.StringVar(&opts.GatewayName, "gateway", "envoy-ai-gateway", "Target Envoy Gateway name")
	fs.StringVar(&opts.GatewayNamespace, "gateway-namespace", "", "Target Envoy Gateway namespace (defaults to --namespace)")
	fs.StringVar(&opts.TargetBackendHost, "backend-host", "", "Override backend service FQDN")
	fs.IntVar(&opts.TargetBackendPort, "backend-port", 8000, "Backend service HTTP port")
	fs.StringVar(&oscarEndpoint, "oscar-endpoint", envOrDefault("OSCAR_ENDPOINT", defaultEndpoint), "OSCAR REST endpoint")
	fs.StringVar(&oscarCluster, "oscar-cluster", envOrDefault("OSCAR_CLUSTER_ID", defaultCluster), "OSCAR cluster ID")
	fs.BoolVar(&opts.Force, "force", false, "Force reapply route manifests")

	flagArgs, posArgs := reorderFlagArgs(args)
	_ = fs.Parse(flagArgs)
	if opts.ModelName == "" && len(posArgs) > 0 {
		opts.ModelName = posArgs[0]
	}
	if opts.ModelName == "" {
		opts.ModelName = defaultModel
		logInfo(fmt.Sprintf("Using default model '%s'.", opts.ModelName))
	}
	if opts.GatewayNamespace == "" {
		opts.GatewayNamespace = opts.Namespace
	}

	ctx, cancel := context.WithTimeout(context.Background(), 45*time.Second)
	defer cancel()

	logStep(fmt.Sprintf("Connecting model '%s' to Gateway '%s/%s'", opts.ModelName, opts.GatewayNamespace, opts.GatewayName))

	settings := config.Settings{
		OscarEndpoint:      oscarEndpoint,
		OscarClusterID:     oscarCluster,
		ModelName:          opts.ModelName,
		Namespace:          opts.Namespace,
		DefaultBackendPort: opts.TargetBackendPort,
	}
	oscarClient := oscar.NewControlPlaneAdapter(settings)
	if info := oscarClient.GetClusterInfo(ctx); info.Connected {
		if svc, _ := oscarClient.GetService(ctx, opts.ModelName); svc != nil {
			logInfo(fmt.Sprintf("OSCAR service '%s' verified in cluster '%s'.", opts.ModelName, oscarCluster))
		} else {
			logWarn(fmt.Sprintf("Service '%s' not found in OSCAR cluster '%s'; continuing with Kubernetes route.", opts.ModelName, oscarCluster))
		}
	}

	endpoints, err := k8s.NewEndpointDiscoveryAdapter(settings).ListEndpoints(ctx, opts.ModelName)
	if err != nil {
		logWarn(fmt.Sprintf("Could not check worker pods yet: %v", err))
	} else {
		logInfo(fmt.Sprintf("Detected %d ready worker endpoint(s) for '%s'.", len(endpoints), opts.ModelName))
	}

	deployer := k8s.NewDefaultKubeDeployer()
	if err := deployer.ConnectModelRoute(ctx, opts); err != nil {
		logErr(err.Error())
		os.Exit(1)
	}

	logInfo(fmt.Sprintf("Model '%s' connected to Envoy Gateway.", opts.ModelName))
	fmt.Printf("  Test with:\n    curl -i -X POST http://localhost:8080/v1/chat/completions -H 'Content-Type: application/json' -H 'x-ai-eg-model: %s' -d '{\"model\":\"%s\",\"messages\":[{\"role\":\"user\",\"content\":\"Hola\"}],\"max_tokens\":30}'\n\n", opts.ModelName, opts.ModelName)
}

func handleModelDisconnect(args []string) {
	defaultNS, defaultModel, _, _ := loadModelDefaults()
	fs := flag.NewFlagSet("model remove", flag.ExitOnError)
	opts := k8s.ModelRouteOptions{}
	fs.StringVar(&opts.Namespace, "namespace", envOrDefault("EPP_NAMESPACE", defaultNS), "Target Kubernetes namespace")
	fs.StringVar(&opts.ModelName, "model", "", "Model name to disconnect")

	flagArgs, posArgs := reorderFlagArgs(args)
	_ = fs.Parse(flagArgs)
	if opts.ModelName == "" && len(posArgs) > 0 {
		opts.ModelName = posArgs[0]
	}
	if opts.ModelName == "" {
		opts.ModelName = defaultModel
	}

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	logStep(fmt.Sprintf("Disconnecting model '%s' from Envoy Gateway (%s)", opts.ModelName, opts.Namespace))
	if err := k8s.NewDefaultKubeDeployer().DisconnectModelRoute(ctx, opts); err != nil {
		logErr(err.Error())
		os.Exit(1)
	}
	logInfo(fmt.Sprintf("Disconnected route and backends for model '%s'.", opts.ModelName))
}

func handleModelList(args []string) {
	defaultNS, defaultModel, defaultEndpoint, defaultCluster := loadModelDefaults()
	fs := flag.NewFlagSet("model list", flag.ExitOnError)
	opts := k8s.StatusOptions{
		GatewayName:         "envoy-ai-gateway",
		ExtensionPolicyName: "envoy-ai-gateway-epp-ext-proc",
		EPPServiceName:      "oscar-inferencemesh-epp",
		EPPHTTPPort:         9091,
		GatewayPort:         8080,
	}
	fs.StringVar(&opts.Namespace, "namespace", envOrDefault("EPP_NAMESPACE", defaultNS), "Target Kubernetes namespace")
	fs.StringVar(&opts.ModelName, "model", envOrDefault("EPP_MODEL_NAME", defaultModel), "Default model filter")
	fs.StringVar(&opts.OscarEndpoint, "oscar-endpoint", envOrDefault("OSCAR_ENDPOINT", defaultEndpoint), "OSCAR REST endpoint")
	fs.StringVar(&opts.OscarClusterID, "oscar-cluster", envOrDefault("OSCAR_CLUSTER_ID", defaultCluster), "OSCAR cluster ID")
	_ = fs.Parse(args)

	settings := config.Settings{
		OscarEndpoint:      opts.OscarEndpoint,
		OscarClusterID:     opts.OscarClusterID,
		ModelName:          opts.ModelName,
		Namespace:          opts.Namespace,
		DefaultBackendPort: 8000,
	}
	inspector := k8s.NewDefaultMeshInspector(oscar.NewControlPlaneAdapter(settings), k8s.NewEndpointDiscoveryAdapter(settings))
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()

	report := inspector.Inspect(ctx, opts)
	logStep(fmt.Sprintf("Connected Models (namespace=%s)", opts.Namespace))
	if len(report.ConnectedModels) == 0 {
		fmt.Println("  (none connected via AIGatewayRoute)")
	}
	for _, cm := range report.ConnectedModels {
		fmt.Printf("  - %-25s route=%s accepted=%s workers=%d/%d endpoints=%d backend=%s\n",
			cm.ModelName, cm.RouteName, emptyFallback(cm.RouteAccepted, "Unknown"),
			cm.ReadyWorkers, cm.TotalWorkers, len(cm.DiscoveredEndpoints), cm.BackendTarget)
	}
	if len(report.UnconnectedModels) > 0 {
		fmt.Printf("\nUnconnected OSCAR/KServe models: %s\n", strings.Join(report.UnconnectedModels, ", "))
		fmt.Printf("Connect with: oscar-im model add <model-name> --namespace %s\n", opts.Namespace)
	}
}
