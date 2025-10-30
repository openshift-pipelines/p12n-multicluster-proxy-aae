# Proxy-AAE Testing Guide

This document provides comprehensive testing instructions for the Proxy-AAE service, including authorization, API endpoints, and WebSocket functionality.

## Prerequisites

- Kubernetes cluster with MultiKueue and Tekton installed
- Proxy-AAE service deployed and running
- Port forwarding to proxy service: `kubectl port-forward -n proxy-aae svc/proxy-aae 8080:8080`
- Running PipelineRun in the cluster

## Test Setup

### 1. Create Test Service Account and RBAC

```bash
# Create test service account
kubectl create serviceaccount test-user -n default

# Create role with necessary permissions
kubectl create role test-user-role \
  --verb=get,list \
  --resource=pipelineruns,taskruns,pods,workloads \
  --resource=pods/log \
  -n default

# Create role binding
kubectl create rolebinding test-user-binding \
  --role=test-user-role \
  --serviceaccount=default:test-user \
  -n default

# Get test token
kubectl create token test-user -n default
```

**Expected Output**: A JWT token that will be used for authorization testing.

## Authorization Testing

### 2. Test Without Token (Should Fail)

```bash
# Test PipelineRun resolution without token
curl -v "http://localhost:8080/api/v1/namespaces/default/pipelineruns/pipeline-test/resolve"
```

**Expected Result**: `403 Forbidden` with message "Access denied: no authorization token provided"

### 3. Test With Invalid Token (Should Fail)

```bash
# Test with invalid token
curl -v -H "Authorization: Bearer invalid-token" \
  "http://localhost:8080/api/v1/namespaces/default/pipelineruns/pipeline-test/resolve"
```

**Expected Result**: `403 Forbidden` with message "failed to create SelfSubjectAccessReview: Unauthorized"

### 4. Test With Valid Token (Should Succeed)

```bash
# Replace TOKEN with the actual token from step 1
curl -H "Authorization: Bearer ${TOKEN}" \
  "http://localhost:8080/api/v1/namespaces/default/pipelineruns/pipeline-test/resolve"
```

**Expected Result**: `200 OK` with worker cluster information:
```json
{"name":"multikueue-test-worker1","state":"Admitted","workloadName":"pipelinerun-pipeline-test-xxx"}
```

## API Endpoint Testing

### 5. Test TaskRuns API

```bash
# Test TaskRuns with valid token
curl -H "Authorization: Bearer ${TOKEN}" \
  "http://localhost:8080/api/v1/namespaces/default/pipelineruns/pipeline-test/taskruns"
```

**Expected Result**: `200 OK` with TaskRuns list containing pipeline tasks

### 6. Test Pods API

```bash
# Test Pods with valid token
curl -H "Authorization: Bearer ${TOKEN}" \
  "http://localhost:8080/api/v1/namespaces/default/pipelineruns/pipeline-test/pods"
```

**Expected Result**: `200 OK` with Pods list containing pipeline pods

### 7. Test Pod Status API

```bash
# Test Pod status with valid token
curl -H "Authorization: Bearer ${TOKEN}" \
  "http://localhost:8080/api/v1/namespaces/default/pods/pipeline-test-echo-pod/status?pipelineRun=pipeline-test"
```

**Expected Result**: `200 OK` with detailed pod status information

### 8. Test Logs API (HTTP)

```bash
# Test logs with valid token
curl -H "Authorization: Bearer ${TOKEN}" \
  "http://localhost:8080/api/v1/namespaces/default/logs?pipelineRun=pipeline-test&pod=pipeline-test-echo-pod&container=step-echo"
```

**Expected Result**: `200 OK` with log content (e.g., "Hello World")

## WebSocket Testing

### 9. Test WebSocket Without Token (Should Fail)

```bash
# Test WebSocket without token
curl -i -N \
  -H "Connection: Upgrade" \
  -H "Upgrade: websocket" \
  -H "Sec-WebSocket-Version: 13" \
  -H "Sec-WebSocket-Key: x3JJHMbDL1EzLkh9GBhXDw==" \
  "http://localhost:8080/api/v1/namespaces/default/logs/stream?pipelineRun=pipeline-test&pod=pipeline-test-echo-pod&container=step-echo"
```

**Expected Result**: `403 Forbidden` with message "Access denied: no authorization token provided"

### 10. Test WebSocket With Valid Token (Should Succeed)

```bash
# Test WebSocket with valid token
curl -i -N \
  -H "Connection: Upgrade" \
  -H "Upgrade: websocket" \
  -H "Sec-WebSocket-Version: 13" \
  -H "Sec-WebSocket-Key: x3JJHMbDL1EzLkh9GBhXDw==" \
  -H "Authorization: Bearer ${TOKEN}" \
  "http://localhost:8080/api/v1/namespaces/default/logs/stream?pipelineRun=pipeline-test&pod=pipeline-test-echo-pod&container=step-echo"
```

**Expected Result**: `101 Switching Protocols` with WebSocket upgrade and log streaming

### 11. Test WebSocket With wscat (Optional)

```bash
# Test WebSocket with wscat (with token)
timeout 5s wscat -c "ws://localhost:8080/api/v1/namespaces/default/logs/stream?pipelineRun=pipeline-test&pod=pipeline-test-echo-pod&container=step-echo"   -H "Authorization: Bearer $TOKEN"
```

**Note**: `wscat` may not properly send Authorization headers during WebSocket upgrades, so results may vary.

## Health Check Testing

### 12. Test Health Endpoints

```bash
# Test health check
curl "http://localhost:8080/health"

# Test readiness check
curl "http://localhost:8080/ready"
```

**Expected Result**: Both should return `200 OK` with "OK" and "Ready" respectively

## Security Validation

### 13. Test Authorization Across All Endpoints

Run the following tests to ensure all endpoints require proper authorization:

```bash
# Test all endpoints without token (should all fail with 403)
curl "http://localhost:8080/api/v1/namespaces/default/pipelineruns/pipeline-test/resolve"
curl "http://localhost:8080/api/v1/namespaces/default/pipelineruns/pipeline-test/taskruns"
curl "http://localhost:8080/api/v1/namespaces/default/pipelineruns/pipeline-test/pods"
curl "http://localhost:8080/api/v1/namespaces/default/pods/pipeline-test-echo-pod/status"
curl "http://localhost:8080/api/v1/namespaces/default/logs?pipelineRun=pipeline-test&pod=pipeline-test-echo-pod&container=step-echo"
```

**Expected Result**: All should return `403 Forbidden`

### 14. Test With Valid Token Across All Endpoints

```bash
# Test all endpoints with valid token (should all succeed with 200)
curl -H "Authorization: Bearer TOKEN" "http://localhost:8080/api/v1/namespaces/default/pipelineruns/pipeline-test/resolve"
curl -H "Authorization: Bearer TOKEN" "http://localhost:8080/api/v1/namespaces/default/pipelineruns/pipeline-test/taskruns"
curl -H "Authorization: Bearer TOKEN" "http://localhost:8080/api/v1/namespaces/default/pipelineruns/pipeline-test/pods"
curl -H "Authorization: Bearer TOKEN" "http://localhost:8080/api/v1/namespaces/default/pods/pipeline-test-echo-pod/status"
curl -H "Authorization: Bearer TOKEN" "http://localhost:8080/api/v1/namespaces/default/logs?pipelineRun=pipeline-test&pod=pipeline-test-echo-pod&container=step-echo"
```

**Expected Result**: All should return `200 OK` with appropriate data

## Expected Test Results Summary

| Test Case | Endpoint | No Token | Invalid Token | Valid Token |
|-----------|-----------|----------|---------------|-------------|
| **PipelineRun Resolution** | `/api/v1/namespaces/{ns}/pipelineruns/{name}/resolve` | 403 Forbidden | 403 Forbidden | 200 OK |
| **TaskRuns API** | `/api/v1/namespaces/{ns}/pipelineruns/{name}/taskruns` | 403 Forbidden | 403 Forbidden | 200 OK |
| **Pods API** | `/api/v1/namespaces/{ns}/pipelineruns/{name}/pods` | 403 Forbidden | 403 Forbidden | 200 OK |
| **Pod Status** | `/api/v1/namespaces/{ns}/pods/{name}/status` | 403 Forbidden | 403 Forbidden | 200 OK |
| **Logs API** | `/api/v1/namespaces/{ns}/logs` | 403 Forbidden | 403 Forbidden | 200 OK |
| **WebSocket Logs** | `/api/v1/namespaces/{ns}/logs/stream` | 403 Forbidden | 403 Forbidden | 101 Switching Protocols |
| **Health Check** | `/health` | 200 OK | 200 OK | 200 OK |
| **Readiness Check** | `/ready` | 200 OK | 200 OK | 200 OK |
