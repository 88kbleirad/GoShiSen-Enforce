# 🛡️ GoShiSen-Enforce

<div align="center">

**A high-performance, stateful firewall engine written in Go**

[![Go Version](https://img.shields.io/github/go-mod/go-version/88kbleirad/GoShiSen-Enforce)](https://golang.org/)
[![License: MIT](https://img.shields.io/badge/License-MIT-yellow.svg)](https://opensource.org/licenses/MIT)
[![Go Report Card](https://goreportcard.com/badge/github.com/88kbleirad/GoShiSen-Enforce)](https://goreportcard.com/report/github.com/88kbleirad/GoShiSen-Enforce)
[![GitHub release](https://img.shields.io/github/v/release/88kbleirad/GoShiSen-Enforce)](https://github.com/88kbleirad/GoShiSen-Enforce/releases)

</div>

---
## Introduction

**GoShiSen-Enforce** is a powerful, stateful firewall engine built from the ground up in Go. It combines the performance of a packet filter with the flexibility of dynamic policy management. Whether you're securing a single server, a microservices cluster, or an edge gateway, GoShiSen-Enforce provides the control and visibility you need.

> The name **GoShiSen** derives from **"ShieldSentry"** – a guardian that stands watch over your network traffic.


## Key Features

| Feature                     | Description                                                                     |
| --------------------------- | ------------------------------------------------------------------------------- |
| **Stateful Inspection**     | Tracks connection states (NEW, ESTABLISHED, RELATED) for intelligent filtering  |
| **Policy-as-Code**          | Define firewall rules using YAML, version-controlled and reproducible           |
| **High Performance**        | Written in Go with Goroutine-based worker pool for concurrent packet processing |
| **Dynamic Rule Management** | Add, update, or delete rules on-the-fly via REST API without restarting         |
| **NAT Support**             | MASQUERADE and DNAT for network address translation                             |
| **Prometheus Metrics**      | Real-time visibility into accepted/dropped packets, connection counts, and more |
| **Web UI**                  | Intuitive dashboard for managing rules and monitoring traffic (optional)        |
| **Cloud-Native Ready**      | Docker support and Kubernetes-ready design                                      |


## Quick Start

### Prerequisites

- **Go 1.21+** for building from source
- **Linux** (recommended) or macOS 
- Root/Administrator privileges 

### Installation

#### Option 1: Download Binary

```bash
wget https://github.com/88kbleirad/GoShiSen-Enforce/releases/latest/download/goshisen-linux-amd64
chmod +x goshisen-linux-amd64
sudo ./goshisen-linux-amd64 --config ./policies/examples/sample.yaml
```

Option 2: Build from Source

```bash
git clone https://github.com/88kbleirad/GoShiSen-Enforce.git
cd GoShiSen-Enforce
make build
sudo ./bin/goshisen --config ./policies/examples/sample.yaml
```

Option 3: Run with Docker

```bash
docker run -it --rm --cap-add=NET_ADMIN -v $(pwd)/policies:/policies \
  dariel/goshisen-enforce:latest \
  --config /policies/examples/sample.yaml
```

## First Rule

Create a policy file policies/my-rules.yaml:

```yaml
rules:
  - id: allow-ssh
    action: ACCEPT
    protocol: TCP
    src: 192.168.1.0/24
    dst: any
    port: 22
    description: "Allow SSH from internal subnet"
    
  - id: allow-https
    action: ACCEPT
    protocol: TCP
    src: any
    dst: 10.0.0.1
    port: 443
    description: "Allow HTTPS to web server"
    
  - id: block-all
    action: DROP
    protocol: any
    src: any
    dst: any
    description: "Default Deny (implicit policy)"
```

Run it:

```bash
sudo ./bin/goshisen --config policies/my-rules.yaml
```

## Documentation
| Document                                                     | Description                                               |
| ------------------------------------------------------------ | --------------------------------------------------------- |
| [Architecture Design](https://./docs/design/architecture.md) | High-level design decisions and system flow               |
| [User Guide](https://./docs/user-guide.md)                   | Complete usage guide with all configuration options       |
| [Developer Guide](https://./docs/developer-guide.md)         | Setup, build, test, and contribution workflow             |
| [API Reference](https://./api/openapi.yaml)                  | REST API endpoints for dynamic rule management            |
| [Security Policy](https://./SECURITY.md)                     | How to report vulnerabilities and security best practices |

## Configuration

GoShiSen-Enforce supports multiple configuration methods:

1. YAML Configuration File
```yaml

interface: eth0                # Network interface to capture
listen: "0.0.0.0:8080"         # REST API listening address
log-level: "info"              # debug, info, warn, error
metrics-port: 9090             # Prometheus metrics port
state-table-timeout: 60        # Connection timeout (seconds)

rules:
  - id: rule-1
    action: ACCEPT
    protocol: TCP
    src: 192.168.1.0/24
    dst: any
    port: 22
```

2. Command Line Flags

```bash
./goshisen \
  --config ./policies/prod.yaml \
  --interface eth0 \
  --api-port 8080 \
  --metrics-port 9090
```

3. Environment Variables

```bash
export GOSHISEN_CONFIG=./policies/prod.yaml
export GOSHISEN_INTERFACE=eth0
./goshisen
```

## Security Best Practices

    Default-Deny Policy: Always end your rule list with a default DROP rule
    Principle of Least Privilege: Only allow traffic that is absolutely necessary
    Regular Audits: Review and update your ruleset periodically
    Enable Logging: Use log-level: info in production for audit trails
    Secure the API: Run the REST API on a management network or use a reverse proxy with authentication

## Monitoring

GoShiSen-Enforce exports Prometheus metrics at /metrics endpoint:

```text
# HELP goshisen_packets_total Total packets processed
# TYPE goshisen_packets_total counter
goshisen_packets_total{action="ACCEPT"} 12345
goshisen_packets_total{action="DROP"} 6789

# HELP goshisen_active_connections Currently active connections
# TYPE goshisen_active_connections gauge
goshisen_active_connections 42
```

## 🤝 Contributing

We welcome contributions! Here's how you can help:

    Report bugs: Open an issue with clear reproduction steps
    Feature requests: Suggest new features with use cases
    Code contributions: Fork the repo, make your changes, and submit a PR

Please read our Contributing Guide and Code of Conduct first.


## License

This project is licensed under the MIT License - see the [LICENSE](./LICENSE) file for details.
