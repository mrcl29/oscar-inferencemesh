#!/usr/bin/env bash
# Reusable logging and console styling utility for development scripts

# Output colors & styling
GREEN='\033[0;32m'
YELLOW='\033[1;33m'
RED='\033[0;31m'
CYAN='\033[0;36m'
BOLD='\033[1m'
NC='\033[0m'

log_info() { echo -e "${GREEN}[INFO]${NC} $1"; }
log_warn() { echo -e "${YELLOW}[WARN]${NC} $1"; }
log_err() { echo -e "${RED}[ERROR]${NC} $1"; }
log_step() { echo -e "\n${CYAN}${BOLD}===> $1${NC}"; }

# Disable post-quantum key agreement in Go to avoid connection resets with Kind/docker-proxy
export GODEBUG="tlsmlkem=0"
