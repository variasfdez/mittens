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
	"flag"
	"fmt"
	"os"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// parseRootFlags registers all flags on a fresh flag set, parses args and returns the result.
func parseRootFlags(t *testing.T, args ...string) Root {
	t.Helper()
	original := flag.CommandLine
	flag.CommandLine = flag.NewFlagSet(os.Args[0], flag.ContinueOnError)
	t.Cleanup(func() { flag.CommandLine = original })

	var root Root
	root.InitFlags()
	require.NoError(t, flag.CommandLine.Parse(args))
	return root
}

func TestRoot_InitFlagsDefaults(t *testing.T) {
	root := parseRootFlags(t)

	assert.Equal(t, 60, root.GetMaxDurationSeconds())
	assert.Equal(t, 30, root.GetMaxReadinessWaitSeconds())
	assert.Equal(t, 30, root.GetMaxWarmupDurationSeconds())
	assert.Equal(t, 2, root.GetConcurrency())
	assert.Equal(t, 0, root.GetConcurrencyTargetSeconds())
	assert.Equal(t, 500, root.RequestDelayMilliseconds)
	assert.False(t, root.ExitAfterWarmup)
	assert.False(t, root.FailReadiness)

	assert.True(t, root.FileProbe.Enabled)
	assert.Equal(t, "alive", root.FileProbe.LivenessPath)
	assert.Equal(t, "ready", root.FileProbe.ReadinessPath)

	assert.Equal(t, "h1", root.Target.HTTPProtocol)
	assert.Equal(t, "http://localhost", root.Target.HTTPHost)
	assert.Equal(t, 8080, root.Target.HTTPPort)
	assert.Equal(t, 10000, root.Target.HTTPTimeoutMilliseconds)
	assert.Equal(t, "localhost", root.Target.GrpcHost)
	assert.Equal(t, 50051, root.Target.GrpcPort)
	assert.Equal(t, 1000, root.Target.GrpcTimeoutMilliseconds)
	assert.Equal(t, "http", root.Target.ReadinessProtocol)
	assert.Equal(t, "/ready", root.Target.ReadinessHTTPPath)
	assert.Equal(t, "http://localhost", root.Target.ReadinessHTTPHost)
	assert.Equal(t, "grpc.health.v1.Health/Check", root.Target.ReadinessGrpcMethod)
	assert.Equal(t, 8080, root.Target.ReadinessPort)
	assert.False(t, root.Target.Insecure)

	assert.Empty(t, root.GetWarmupHTTPHeaders())
	assert.Empty(t, root.HTTP.Requests)
	assert.Equal(t, "", root.HTTP.Compression)
	assert.Empty(t, root.Grpc.Requests)
}

func TestRoot_InitFlagsParsesCommandLine(t *testing.T) {
	root := parseRootFlags(t,
		"-max-duration-seconds=120",
		"-max-readiness-wait-seconds=15",
		"-max-warmup-seconds=45",
		"-concurrency=5",
		"-request-delay-milliseconds=100",
		"-concurrency-target-seconds=10",
		"-exit-after-warmup",
		"-fail-readiness",
		"-file-probe-enabled=false",
		"-file-probe-liveness-path=/tmp/alive",
		"-file-probe-readiness-path=/tmp/ready",
		"-target-http-protocol=h2c",
		"-target-http-host=http://service",
		"-target-http-port=9090",
		"-target-http-timeout-milliseconds=2000",
		"-target-grpc-host=grpc-service",
		"-target-grpc-port=6565",
		"-target-grpc-timeout-milliseconds=3000",
		"-target-readiness-protocol=grpc",
		"-target-readiness-http-path=/health",
		"-target-readiness-http-host=http://readiness",
		"-target-readiness-grpc-method=health/Check",
		"-target-readiness-port=9091",
		"-target-insecure",
		"-http-headers=X-Foo: bar",
		"-http-headers=X-Bar: baz",
		"-http-requests=get:/ping",
		"-http-requests=post:/echo:{}",
		"-http-requests-compression=gzip",
		"-grpc-requests=svc/method",
	)

	assert.Equal(t, 120, root.GetMaxDurationSeconds())
	assert.Equal(t, 15, root.GetMaxReadinessWaitSeconds())
	assert.Equal(t, 45, root.GetMaxWarmupDurationSeconds())
	assert.Equal(t, 5, root.GetConcurrency())
	assert.Equal(t, 100, root.RequestDelayMilliseconds)
	assert.Equal(t, 10, root.GetConcurrencyTargetSeconds())
	assert.True(t, root.ExitAfterWarmup)
	assert.True(t, root.FailReadiness)

	assert.False(t, root.FileProbe.Enabled)
	assert.Equal(t, "/tmp/alive", root.FileProbe.LivenessPath)
	assert.Equal(t, "/tmp/ready", root.FileProbe.ReadinessPath)

	assert.Equal(t, "h2c", root.Target.HTTPProtocol)
	assert.Equal(t, "http://service", root.Target.HTTPHost)
	assert.Equal(t, 9090, root.Target.HTTPPort)
	assert.Equal(t, 2000, root.Target.HTTPTimeoutMilliseconds)
	assert.Equal(t, "grpc-service", root.Target.GrpcHost)
	assert.Equal(t, 6565, root.Target.GrpcPort)
	assert.Equal(t, 3000, root.Target.GrpcTimeoutMilliseconds)
	assert.Equal(t, "grpc", root.Target.ReadinessProtocol)
	assert.Equal(t, "/health", root.Target.ReadinessHTTPPath)
	assert.Equal(t, "http://readiness", root.Target.ReadinessHTTPHost)
	assert.Equal(t, "health/Check", root.Target.ReadinessGrpcMethod)
	assert.Equal(t, 9091, root.Target.ReadinessPort)
	assert.True(t, root.Target.Insecure)

	assert.Equal(t, []string{"X-Foo: bar", "X-Bar: baz"}, root.GetWarmupHTTPHeaders())
	assert.Equal(t, stringArray{"get:/ping", "post:/echo:{}"}, root.HTTP.Requests)
	assert.Equal(t, "gzip", root.HTTP.Compression)
	assert.Equal(t, stringArray{"svc/method"}, root.Grpc.Requests)
}

func TestRoot_GetWarmupTargetOptions(t *testing.T) {
	for _, protocol := range []string{"http", "grpc"} {
		t.Run(protocol, func(t *testing.T) {
			root := Root{Target: Target{
				ReadinessProtocol:   protocol,
				ReadinessHTTPPath:   "/ready",
				ReadinessGrpcMethod: "health/Check",
				ReadinessPort:       8080,
			}}

			options, err := root.GetWarmupTargetOptions()

			require.NoError(t, err)
			assert.Equal(t, protocol, options.ReadinessProtocol)
			assert.Equal(t, "/ready", options.ReadinessHTTPPath)
			assert.Equal(t, "health/Check", options.ReadinessGrpcMethod)
			assert.Equal(t, 8080, options.ReadinessPort)
		})
	}
}

func TestRoot_GetWarmupTargetOptionsUnsupportedProtocol(t *testing.T) {
	root := Root{Target: Target{ReadinessProtocol: "tcp"}}

	options, err := root.GetWarmupTargetOptions()

	require.Error(t, err)
	assert.Equal(t, "readiness protocol tcp not supported, please use http or grpc", err.Error())
	assert.Equal(t, "tcp", options.ReadinessProtocol)
}

func TestRoot_GetWarmupHTTPRequests(t *testing.T) {
	root := Root{HTTP: HTTP{Requests: stringArray{"get:/ping", "post:/echo:hello"}, Compression: "gzip"}}

	requests, err := root.GetWarmupHTTPRequests()

	require.NoError(t, err)
	require.Len(t, requests, 2)
	assert.Equal(t, "GET", requests[0].Method)
	assert.Equal(t, "/ping", requests[0].Path)
	assert.Nil(t, requests[0].Body)
	assert.Equal(t, "POST", requests[1].Method)
	assert.Equal(t, "/echo", requests[1].Path)
	assert.Equal(t, "gzip", requests[1].Headers["Content-Encoding"])
}

func TestRoot_GetWarmupHTTPRequestsInvalid(t *testing.T) {
	root := Root{HTTP: HTTP{Requests: stringArray{"get:/ping", "get/invalid"}}}

	requests, err := root.GetWarmupHTTPRequests()

	require.Error(t, err)
	assert.Equal(t, "invalid request flag: get/invalid, expected format <http-method>:<path>[:body]", err.Error())
	assert.Nil(t, requests)
}

func TestRoot_GetWarmupGrpcRequests(t *testing.T) {
	root := Root{Grpc: Grpc{Requests: stringArray{"svc/ping", "svc/echo:{\"key\":\"value\"}"}}}

	requests, err := root.GetWarmupGrpcRequests()

	require.NoError(t, err)
	require.Len(t, requests, 2)
	assert.Equal(t, "svc/ping", requests[0].ServiceMethod)
	assert.Equal(t, "", requests[0].Message)
	assert.Equal(t, "svc/echo", requests[1].ServiceMethod)
	assert.Equal(t, "{\"key\":\"value\"}", requests[1].Message)
}

func TestRoot_GetWarmupGrpcRequestsInvalid(t *testing.T) {
	root := Root{Grpc: Grpc{Requests: stringArray{"svc/ping", "invalid"}}}

	requests, err := root.GetWarmupGrpcRequests()

	require.Error(t, err)
	assert.Equal(t, "invalid request flag: invalid, expected format <service>/<method>[:body]", err.Error())
	assert.Nil(t, requests)
}

func TestRoot_ClientsUseTargetSettings(t *testing.T) {
	warmupPaths, warmupHost, warmupPort := newRecordingServer(t)
	readinessPaths, readinessHost, readinessPort := newRecordingServer(t)
	root := Root{Target: Target{
		HTTPProtocol:            "h1",
		HTTPHost:                warmupHost,
		HTTPPort:                warmupPort,
		HTTPTimeoutMilliseconds: 1000,
		ReadinessHTTPHost:       readinessHost,
		ReadinessPort:           readinessPort,
		GrpcHost:                "grpc.local",
		GrpcPort:                50051,
	}}

	require.NoError(t, root.GetHTTPClient().SendRequest("GET", "/warmup", nil, nil).Err)
	require.NoError(t, root.GetReadinessHTTPClient().SendRequest("GET", "/ready", nil, nil).Err)
	assert.Equal(t, []string{"/warmup"}, *warmupPaths)
	assert.Equal(t, []string{"/ready"}, *readinessPaths)

	assert.Contains(t, fmt.Sprintf("%+v", root.GetGrpcClient()), "host:grpc.local:50051 ")
	assert.Contains(t, fmt.Sprintf("%+v", root.GetReadinessGrpcClient()), fmt.Sprintf("host:grpc.local:%d ", readinessPort))
}

func TestRoot_String(t *testing.T) {
	root := Root{
		Concurrency: 3,
		FileProbe:   FileProbe{LivenessPath: "alive"},
		HTTP:        HTTP{Compression: "gzip"},
		HTTPHeaders: HTTPHeaders{Headers: stringArray{"X-Foo: bar"}},
		Grpc:        Grpc{Requests: stringArray{"svc/ping"}},
	}

	assert.Contains(t, root.String(), "Concurrency:3")
	assert.Contains(t, root.FileProbe.String(), "LivenessPath:alive")
	assert.Contains(t, root.HTTP.String(), "Compression:gzip")
	assert.Contains(t, root.HTTPHeaders.String(), "X-Foo: bar")
	assert.Contains(t, root.Grpc.String(), "svc/ping")
}
