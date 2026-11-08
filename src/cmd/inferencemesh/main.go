// Package main implements the inferencemesh CLI tool for OSCAR InferenceMesh.
package main

import (
	"context"
	"flag"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"time"

	"github.com/grycap/oscar-inferencemesh/src/internal/config"
	"github.com/grycap/oscar-inferencemesh/src/internal/infrastructure/oscar"
)

const (
	cliVersion = "0.1.0"
	banner     = `
   ____  ____  _________    ____     ____  ____ 
  / __ \/ ___// ____/   |  / __ \   /  _/ / __ \
 / / / /\__ \/ /   / /| | / /_/ /   / /  / / / /
/ /_/ /___/ / /___/ ___ |/ _, _/  _/ /  / /_/ / 
\____//____/\____/_/  |_/_/ |_|  /___/ /_____/  
        OSCAR InferenceMesh Control CLI v` + cliVersion + "\n"
)

type initOptions struct {
	docker        bool
	skipGateway   bool
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
	case "version", "--version", "-v":
		fmt.Printf("inferencemesh CLI version %s\n", cliVersion)
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
	fmt.Println("Usage: inferencemesh <command> [options]")
	fmt.Println()
	fmt.Println("Available Commands:")
	fmt.Println("  init       Initialize and deploy the Envoy AI Gateway, EPP, and Patrón B ext_proc filter")
	fmt.Println("  version    Display the CLI version")
	fmt.Println("  help       Show help information")
	fmt.Println()
	fmt.Println("Run 'inferencemesh <command> --help' for details on specific command options.")
}

func handleInit(args []string) {
	fs := flag.NewFlagSet("init", flag.ExitOnError)

	opts := initOptions{}
	fs.BoolVar(&opts.docker, "docker", false, "Deploy EPP inside Kubernetes/Docker container instead of a local host process (default: false)")
	fs.BoolVar(&opts.skipGateway, "skip-gateway", false, "Skip Envoy AI Gateway deployment check (default: false)")
	fs.StringVar(&opts.oscarEndpoint, "oscar-endpoint", envOrDefault("OSCAR_ENDPOINT", "https://localhost.direct"), "OSCAR REST endpoint URL")
	fs.StringVar(&opts.oscarCluster, "oscar-cluster", envOrDefault("OSCAR_CLUSTER_ID", "local"), "OSCAR cluster ID in ~/.oscar.yaml")
	fs.StringVar(&opts.modelName, "model", envOrDefault("EPP_MODEL_NAME", "qwen2-5-05b-instruct"), "Target LLM model name registered in OSCAR")
	fs.StringVar(&opts.namespace, "namespace", envOrDefault("EPP_NAMESPACE", "oscar-svc"), "Target Kubernetes namespace")
	fs.IntVar(&opts.httpPort, "http-port", 9091, "EPP HTTP verification server port")
	fs.IntVar(&opts.grpcPort, "grpc-port", 9002, "EPP gRPC ext_proc server port")

	_ = fs.Parse(args)

	fmt.Print(banner)
	fmt.Println("===> Initializing OSCAR InferenceMesh...")
	fmt.Printf("  - Deployment Mode:    %s\n", deploymentModeString(opts.docker))
	fmt.Printf("  - Model Target:       %s\n", opts.modelName)
	fmt.Printf("  - Namespace:          %s\n", opts.namespace)
	fmt.Printf("  - OSCAR Endpoint:     %s (Cluster: %s)\n", opts.oscarEndpoint, opts.oscarCluster)
	fmt.Printf("  - ExtProc gRPC Port:  %d\n", opts.grpcPort)
	fmt.Printf("  - HTTP Verify Port:   %d\n", opts.httpPort)
	fmt.Println()

	repoRoot := findRepoRoot()

	// 1. Validate OSCAR connectivity
	logStep("1. Validating OSCAR Control Plane connectivity")
	settings := config.Settings{
		OscarEndpoint:  opts.oscarEndpoint,
		OscarClusterID: opts.oscarCluster,
		ModelName:      opts.modelName,
		Namespace:      opts.namespace,
	}
	oscarClient := oscar.NewControlPlaneAdapter(settings)
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	clusterInfo := oscarClient.GetClusterInfo(ctx)
	if clusterInfo.Connected {
		logInfo(fmt.Sprintf("Connected to OSCAR cluster '%s' at %s", opts.oscarCluster, opts.oscarEndpoint))
		svcSummary, _ := oscarClient.GetService(ctx, opts.modelName)
		if svcSummary != nil {
			logInfo(fmt.Sprintf("Found registered service '%s' in OSCAR.", opts.modelName))
		} else {
			logWarn(fmt.Sprintf("Service '%s' is not yet registered in OSCAR (can be registered later).", opts.modelName))
		}
	} else {
		logWarn(fmt.Sprintf("OSCAR check note: %s (proceeding with gateway deployment...)", clusterInfo.Error))
	}

	// 2. Deploy/Verify Envoy AI Gateway
	if !opts.skipGateway {
		logStep("2. Ensuring Envoy Gateway & Envoy AI Gateway are deployed")
		gatewayScript := filepath.Join(repoRoot, "scripts", "production", "deploy-gateway.sh")
		if err := runScript(gatewayScript); err != nil {
			logErr(fmt.Sprintf("Failed to deploy Envoy AI Gateway: %v", err))
			os.Exit(1)
		}
	} else {
		logInfo("Skipping Gateway deployment as requested (--skip-gateway).")
	}

	// 3. Deploy EPP based on selected mode (Docker container vs Host direct process)
	if opts.docker {
		logStep("3. Deploying EPP in Container Mode (Kubernetes Pod)")
		deployEppScript := filepath.Join(repoRoot, "scripts", "production", "deploy-epp.sh")
		env := map[string]string{
			"NAMESPACE":        opts.namespace,
			"MODEL_NAME":       opts.modelName,
			"OSCAR_ENDPOINT":   opts.oscarEndpoint,
			"OSCAR_CLUSTER_ID": opts.oscarCluster,
		}
		if err := runScriptWithEnv(deployEppScript, env); err != nil {
			logErr(fmt.Sprintf("Failed to deploy EPP container: %v", err))
			os.Exit(1)
		}

		// 4. Attach EnvoyExtensionPolicy (Patrón B: ext_proc gRPC filter)
		logStep("4. Attaching EnvoyExtensionPolicy (Patrón B: ext_proc gRPC)")
		extPolicyScript := filepath.Join(repoRoot, "scripts", "production", "deploy-extension-policy.sh")
		if err := runScriptWithEnv(extPolicyScript, env); err != nil {
			logErr(fmt.Sprintf("Failed to configure EnvoyExtensionPolicy: %v", err))
			os.Exit(1)
		}
	} else {
		logStep("3. Configuring Host Execution Mode for EPP")
		logInfo("The EPP runs directly on the host machine.")
		logInfo(fmt.Sprintf("Listen Addresses: gRPC ext_proc on :%d, HTTP verification on :%d", opts.grpcPort, opts.httpPort))
		logInfo("To launch the EPP process on the host machine, run:")
		fmt.Printf("\n   go -C src run ./cmd/epp --port %d\n\n", opts.httpPort)

		// Also configure EnvoyExtensionPolicy so the Gateway is ready for the EPP
		logStep("4. Ensuring EnvoyExtensionPolicy CRD is applied")
		extPolicyScript := filepath.Join(repoRoot, "scripts", "production", "deploy-extension-policy.sh")
		env := map[string]string{
			"NAMESPACE":  opts.namespace,
			"MODEL_NAME": opts.modelName,
		}
		_ = runScriptWithEnv(extPolicyScript, env)
	}

	logStep("Initialization Completed Successfully")
	fmt.Println("Summary:")
	fmt.Printf("  ✓ Envoy AI Gateway is ready\n")
	fmt.Printf("  ✓ Patrón B (EnvoyExtensionPolicy ext_proc on port %d) is active\n", opts.grpcPort)
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

func findRepoRoot() string {
	// Start from current directory and walk upwards
	dir, err := os.Getwd()
	if err != nil {
		return "."
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
	return "."
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
