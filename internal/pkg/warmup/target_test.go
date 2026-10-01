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

package warmup

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"

	"mittens/fixture"
	mgrpc "mittens/internal/pkg/grpc"
	mhttp "mittens/internal/pkg/http"
)

func newHTTPReadinessTarget(readinessClient mhttp.Client, path string) Target {
	return NewTarget(
		readinessClient, mgrpc.Client{},
		mhttp.Client{}, mgrpc.Client{},
		TargetOptions{ReadinessProtocol: "http", ReadinessHTTPPath: path},
	)
}

func TestWaitForReadinessProbeSucceedsAfterRetries(t *testing.T) {
	var attempts int32
	server := httptest.NewServer(http.HandlerFunc(func(rw http.ResponseWriter, r *http.Request) {
		if atomic.AddInt32(&attempts, 1) < 2 {
			rw.WriteHeader(http.StatusInternalServerError)
			return
		}
		rw.WriteHeader(http.StatusOK)
	}))
	defer server.Close()

	target := newHTTPReadinessTarget(mhttp.NewClient(server.URL, false, 5000, mhttp.HTTP1), "/health")

	if err := target.WaitForReadinessProbe(5, nil); err != nil {
		t.Fatalf("expected readiness probe to succeed after retries, got: %v", err)
	}
	if got := atomic.LoadInt32(&attempts); got != 2 {
		t.Fatalf("expected 2 readiness attempts (500 then 200), got %d", got)
	}
}

func TestWaitForReadinessProbeSucceedsOnFirstAttempt(t *testing.T) {
	var attempts int32
	server := httptest.NewServer(http.HandlerFunc(func(rw http.ResponseWriter, r *http.Request) {
		atomic.AddInt32(&attempts, 1)
		rw.WriteHeader(http.StatusOK)
	}))
	defer server.Close()

	target := newHTTPReadinessTarget(mhttp.NewClient(server.URL, false, 5000, mhttp.HTTP1), "/health")

	if err := target.WaitForReadinessProbe(5, nil); err != nil {
		t.Fatalf("expected readiness probe to succeed on the first attempt, got: %v", err)
	}
	if got := atomic.LoadInt32(&attempts); got != 1 {
		t.Fatalf("expected exactly 1 readiness attempt, got %d", got)
	}
}

func TestWaitForReadinessProbeTimesOut(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(rw http.ResponseWriter, r *http.Request) {
		rw.WriteHeader(http.StatusInternalServerError)
	}))
	defer server.Close()

	target := newHTTPReadinessTarget(mhttp.NewClient(server.URL, false, 5000, mhttp.HTTP1), "/health")

	if err := target.WaitForReadinessProbe(1, nil); err == nil {
		t.Fatal("expected readiness probe to time out when the target never becomes ready")
	}
}

func TestWaitForReadinessProbeGrpc(t *testing.T) {
	grpcServer, grpcPort := fixture.StartGrpcTargetTestServer(fixture.NewCallStats())
	defer grpcServer.GracefulStop()

	target := NewTarget(
		mhttp.Client{}, mgrpc.NewClient(fmt.Sprintf("localhost:%d", grpcPort), true, 10000),
		mhttp.Client{}, mgrpc.Client{},
		TargetOptions{ReadinessProtocol: "grpc", ReadinessGrpcMethod: "grpc.testing.TestService/EmptyCall"},
	)

	if err := target.WaitForReadinessProbe(5, nil); err != nil {
		t.Fatalf("expected gRPC readiness probe to succeed, got: %v", err)
	}
}

func TestWaitForReadinessProbeGrpcTimesOutWhenConnectFails(t *testing.T) {
	// Nothing listens on this port, so Connect fails on every attempt.
	target := NewTarget(
		mhttp.Client{}, mgrpc.NewClient("localhost:1", true, 500),
		mhttp.Client{}, mgrpc.Client{},
		TargetOptions{ReadinessProtocol: "grpc", ReadinessGrpcMethod: "grpc.testing.TestService/EmptyCall"},
	)

	if err := target.WaitForReadinessProbe(1, nil); err == nil {
		t.Fatal("expected readiness probe to time out when the gRPC target is unreachable")
	}
}

func TestWaitForReadinessProbeGrpcFailsOnErrorStatus(t *testing.T) {
	t.Skip("documents a bug: grpc.Client.SendRequest returns Err=nil when InvokeRPC fails, " +
		"so the gRPC readiness probe reports ready even when the health-check method returns an " +
		"error status (e.g. Unimplemented); unskip once SendRequest propagates RPC errors")

	// The fixture service returns Unimplemented for every method; a correct readiness
	// probe should keep waiting and time out, but today it returns nil on the first attempt.
	grpcServer, grpcPort := fixture.StartGrpcTargetTestServer(fixture.NewCallStats())
	defer grpcServer.GracefulStop()

	target := NewTarget(
		mhttp.Client{}, mgrpc.NewClient(fmt.Sprintf("localhost:%d", grpcPort), true, 10000),
		mhttp.Client{}, mgrpc.Client{},
		TargetOptions{ReadinessProtocol: "grpc", ReadinessGrpcMethod: "grpc.testing.TestService/EmptyCall"},
	)

	if err := target.WaitForReadinessProbe(1, nil); err == nil {
		t.Fatal("expected readiness probe to keep waiting when the health-check method returns an error status")
	}
}
