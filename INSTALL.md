# Installation Guide

## Prerequisites

### Install ko

```bash
# Install ko (Kubernetes Object Builder)
go install github.com/google/ko@latest

# Or using curl
curl -L https://github.com/google/ko/releases/latest/download/ko_Linux_x86_64.tar.gz | tar xzf - ko
sudo mv ko /usr/local/bin/
```

### Verify ko installation

```bash
ko version
```

## Quick Start

### 1. Clone the repository

```bash
git clone https://github.com/khrm/proxy-aae.git
cd proxy-aae
```

### 2. SET UP KO_DOCKER_REPO

```bash
export KO_DOCKER_REPO="kind.local"
```

### 3. Deploy to Kubernetes

```bash
ko apply -R -f config/
```


### 4. Test the deployment

```bash
# Port forward to access the service
make port-forward

# Test the health endpoint
curl http://localhost:8080/health

# Test the ready endpoint
curl http://localhost:8080/ready
```


## Configuration

### Environment Variables

The following environment variables can be configured:

- `REQUEST_TIMEOUT`: Timeout for worker cluster requests (default: `30s`)
- `DEFAULT_LOG_TAIL_LINES`: Default number of log lines to tail (default: `100`)
- `LOG_LEVEL`: Logging level (default: `2`)

