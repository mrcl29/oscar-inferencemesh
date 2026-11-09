// Package main implements the oscar-im CLI tool for OSCAR InferenceMesh.
package main

import (
	"context"
	"flag"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
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
	docker        bool
	skipGateway   bool
	force         bool
	oscarEndpoint string
	oscarCluster  string
	modelName     string
	namespace     string
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
		handleInit(os.Args[2:])
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
	fmt.Println("  init       Initialize and deploy the Envoy AI Gateway, EPP, and ext_proc filter")
	fmt.Println("  config     View and edit persistent configuration (~/.config/oscar-im/config.yaml)")
	fmt.Println("  version    Display the CLI version")
	fmt.Println("  help       Show help information")
	fmt.Println()
	fmt.Println("Run 'oscar-im <command> --help' for details on specific command options.")
}

func handleConfig(args []string) {
	if err := config.RunConfigCommand(args, os.Stdin, os.Stdout); err != nil {
		logErr(err.Error())
		os.Exit(1)
	}
}

func handleInit(args []string) {
	fs := flag.NewFlagSet("init", flag.ExitOnError)

	fileCfg, _ := config.LoadOrCreateFileConfig("")
	defaultEndpoint := "https://localhost.direct"
	defaultCluster := "local"
	defaultModel := "qwen2-5-05b-instruct"
	defaultNamespace := "oscar-svc"
	defaultHTTPPort := 9091
	defaultGRPCPort := 9002
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
	fs.BoolVar(&opts.force, "force", false, "Force rebuild and redeployment even if components are already running and available")
	fs.StringVar(&opts.oscarEndpoint, "oscar-endpoint", envOrDefault("OSCAR_ENDPOINT", defaultEndpoint), "OSCAR REST endpoint URL")
	fs.StringVar(&opts.oscarCluster, "oscar-cluster", envOrDefault("OSCAR_CLUSTER_ID", defaultCluster), "OSCAR cluster ID in configuration")
	fs.StringVar(&opts.modelName, "model", envOrDefault("EPP_MODEL_NAME", defaultModel), "Target LLM model name registered in OSCAR")
	fs.StringVar(&opts.namespace, "namespace", envOrDefault("EPP_NAMESPACE", defaultNamespace), "Target Kubernetes namespace")
	fs.IntVar(&opts.httpPort, "http-port", defaultHTTPPort, "EPP HTTP verification server port")
	fs.IntVar(&opts.grpcPort, "grpc-port", defaultGRPCPort, "EPP gRPC ext_proc server port")

	_ = fs.Parse(args)

	fmt.Print(banner)
	fmt.Println("===> Initializing OSCAR-IM...")
	fmt.Printf("  - Deployment Mode:    %s\n", deploymentModeString(opts.docker))
	fmt.Printf("  - Model Target:       %s\n", opts.modelName)
	fmt.Printf("  - Namespace:          %s\n", opts.namespace)
	fmt.Printf("  - OSCAR Endpoint:     %s (Cluster: %s)\n", opts.oscarEndpoint, opts.oscarCluster)
	fmt.Printf("  - ExtProc gRPC Port:  %d\n", opts.grpcPort)
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

	// 3. Deploy/Verify Envoy AI Gateway
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
			logInfo("Envoy Gateway & Envoy AI Gateway are deployed and Available.")
		}
	} else {
		logInfo("Skipping Gateway deployment as requested (--skip-gateway).")
	}

	// 4. Deploy EPP based on selected mode (Docker container vs Host direct process)
	if opts.docker {
		logStep("4. Deploying EPP in Container Mode (Kubernetes Pod)")
		deployEppScript := filepath.Join(repoRoot, "scripts", "production", "deploy-epp.sh")
		if fileExists(deployEppScript) {
			env := map[string]string{
				"NAMESPACE":        opts.namespace,
				"MODEL_NAME":       opts.modelName,
				"OSCAR_ENDPOINT":   opts.oscarEndpoint,
				"OSCAR_CLUSTER_ID": opts.oscarCluster,
				"FORCE":            strconv.FormatBool(opts.force),
			}
			if err := runScriptWithEnv(deployEppScript, env); err != nil {
				logErr(fmt.Sprintf("Failed to deploy EPP container: %v", err))
				os.Exit(1)
			}
		} else {
			logInfo("Running in standalone binary mode (using embedded EPP deployment manifest)...")
			eppOpts := k8s.EPPDeployOptions{
				Namespace:      opts.namespace,
				ModelName:      opts.modelName,
				EPPImage:       envOrDefault("EPP_IMAGE", "oscar-inferencemesh-epp:latest"),
				OscarEndpoint:  opts.oscarEndpoint,
				OscarClusterID: opts.oscarCluster,
				OscarVerifySSL: envOrDefault("OSCAR_VERIFY_SSL", "false"),
				Force:          opts.force,
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
	fmt.Printf("  ✓ Envoy AI Gateway is ready\n")
	fmt.Printf("  ✓ EnvoyExtensionPolicy ext_proc on port %d is active\n", opts.grpcPort)
	if opts.docker {
		fmt.Printf("  ✓ EPP is deployed as a Kubernetes Service in namespace '%s'\n", opts.namespace)
	} else {
		fmt.Printf("  ✓ EPP is configured for host execution\n")
	}
	fmt.Printf("  ✓ Ready to process model '%s'\n\n", opts.modelName)
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

func runScriptWithEnv(scriptPath string, extraEnv map[string]string) error {
	cmd := exec.Command("/usr/bin/env", "bash", scriptPath)
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
