//Copyright 2026 Expedia, Inc.
//
//Licensed under the Apache License, Version 2.0 (the "License");
//you may not use this file except in compliance with the License.
//You may obtain a copy of the License at
//
//http://www.apache.org/licenses/LICENSE-2.0
//
//Unless required by applicable law or agreed to in writing, software
//distributed under the License is distributed on an "AS IS" BASIS,
//WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
//See the License for the specific language governing permissions and
//limitations under the License.

package flags

import (
	"fmt"
	nethttp "net/http"
	"net/http/httptest"
	"net/url"
	"strconv"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestTarget_ToIntOrDefaultIfNull(t *testing.T) {
	value := 9090

	assert.Equal(t, 9090, toIntOrDefaultIfNull(&value, 8080))
	assert.Equal(t, 8080, toIntOrDefaultIfNull(nil, 8080))
}

func TestTarget_ToStringOrDefaultIfNull(t *testing.T) {
	value := "http://example.com"

	assert.Equal(t, "http://example.com", toStringOrDefaultIfNull(&value, "http://localhost"))
	assert.Equal(t, "http://localhost", toStringOrDefaultIfNull(nil, "http://localhost"))
}

func TestTarget_GetWarmupTargetOptions(t *testing.T) {
	target := Target{
		ReadinessProtocol:   "grpc",
		ReadinessHTTPPath:   "/health",
		ReadinessGrpcMethod: "grpc.health.v1.Health/Check",
		ReadinessPort:       6565,
	}

	options := target.getWarmupTargetOptions()

	assert.Equal(t, "grpc", options.ReadinessProtocol)
	assert.Equal(t, "/health", options.ReadinessHTTPPath)
	assert.Equal(t, "grpc.health.v1.Health/Check", options.ReadinessGrpcMethod)
	assert.Equal(t, 6565, options.ReadinessPort)
}

func TestTarget_HTTPClientsUseConfiguredHostAndPort(t *testing.T) {
	warmupServer, warmupHost, warmupPort := newRecordingServer(t)
	readinessServer, readinessHost, readinessPort := newRecordingServer(t)

	target := Target{
		HTTPProtocol:            "h1",
		HTTPHost:                warmupHost,
		HTTPPort:                warmupPort,
		HTTPTimeoutMilliseconds: 1000,
		ReadinessHTTPHost:       readinessHost,
		ReadinessPort:           readinessPort,
	}

	warmupResp := target.getHTTPClient().SendRequest(nethttp.MethodGet, "/warmup", nil, nil)
	require.NoError(t, warmupResp.Err)
	assert.Equal(t, nethttp.StatusOK, warmupResp.StatusCode)

	readinessResp := target.getReadinessHTTPClient().SendRequest(nethttp.MethodGet, "/ready", nil, nil)
	require.NoError(t, readinessResp.Err)
	assert.Equal(t, nethttp.StatusOK, readinessResp.StatusCode)

	assert.Equal(t, []string{"/warmup"}, *warmupServer)
	assert.Equal(t, []string{"/ready"}, *readinessServer)
}

func TestTarget_GrpcClientsUseConfiguredHostAndPort(t *testing.T) {
	target := Target{
		GrpcHost:                "grpc.local",
		GrpcPort:                50051,
		GrpcTimeoutMilliseconds: 1500,
		ReadinessPort:           6565,
		Insecure:                true,
	}

	grpcClient := fmt.Sprintf("%+v", target.getGrpcClient())
	assert.Contains(t, grpcClient, "host:grpc.local:50051 ")
	assert.Contains(t, grpcClient, "insecure:true ")
	assert.Contains(t, grpcClient, "timeoutMilliseconds:1500 ")

	readinessClient := fmt.Sprintf("%+v", target.getReadinessGrpcClient())
	assert.Contains(t, readinessClient, "host:grpc.local:6565 ")
	assert.Contains(t, readinessClient, "insecure:true ")
	assert.Contains(t, readinessClient, "timeoutMilliseconds:1500 ")
}

func TestTarget_String(t *testing.T) {
	target := Target{HTTPHost: "http://localhost", HTTPPort: 8080}

	assert.Contains(t, target.String(), "HTTPHost:http://localhost")
	assert.Contains(t, target.String(), "HTTPPort:8080")
}

// newRecordingServer starts an HTTP server that records every requested path and
// returns the recorded paths together with the host (scheme included) and port it listens on.
func newRecordingServer(t *testing.T) (*[]string, string, int) {
	t.Helper()
	paths := &[]string{}
	server := httptest.NewServer(nethttp.HandlerFunc(func(w nethttp.ResponseWriter, r *nethttp.Request) {
		*paths = append(*paths, r.URL.Path)
		w.WriteHeader(nethttp.StatusOK)
	}))
	t.Cleanup(server.Close)

	serverURL, err := url.Parse(server.URL)
	require.NoError(t, err)
	port, err := strconv.Atoi(serverURL.Port())
	require.NoError(t, err)

	return paths, fmt.Sprintf("%s://%s", serverURL.Scheme, serverURL.Hostname()), port
}
