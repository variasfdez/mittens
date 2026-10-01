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
	"sync"
	"testing"
	"time"

	"mittens/fixture"
	mgrpc "mittens/internal/pkg/grpc"
	mhttp "mittens/internal/pkg/http"
)

func containsHTTPRequest(requests []mhttp.Request, r mhttp.Request) bool {
	for _, want := range requests {
		if want.Method == r.Method && want.Path == r.Path {
			return true
		}
	}
	return false
}

func containsGrpcRequest(requests []mgrpc.Request, r mgrpc.Request) bool {
	for _, want := range requests {
		if want.ServiceMethod == r.ServiceMethod && want.Message == r.Message {
			return true
		}
	}
	return false
}

func TestGetWarmupHTTPRequestsEmptySliceClosesImmediately(t *testing.T) {
	w := Warmup{}

	select {
	case _, ok := <-w.GetWarmupHTTPRequests(60):
		if ok {
			t.Fatal("expected channel to be closed immediately when there are no requests")
		}
	case <-time.After(2 * time.Second):
		t.Fatal("expected channel to close immediately when there are no requests")
	}
}

func TestGetWarmupGrpcRequestsEmptySliceClosesImmediately(t *testing.T) {
	w := Warmup{}

	select {
	case _, ok := <-w.GetWarmupGrpcRequests(60):
		if ok {
			t.Fatal("expected channel to be closed immediately when there are no requests")
		}
	case <-time.After(2 * time.Second):
		t.Fatal("expected channel to close immediately when there are no requests")
	}
}

func TestGetWarmupHTTPRequestsEmitsOnlyConfiguredRequestsAndCloses(t *testing.T) {
	requests := []mhttp.Request{
		{Method: http.MethodGet, Path: "/a"},
		{Method: http.MethodPost, Path: "/b"},
	}
	w := Warmup{HttpRequests: requests}

	requestsChan := w.GetWarmupHTTPRequests(1)
	emitted := 0
	deadline := time.After(5 * time.Second)

	for {
		select {
		case r, ok := <-requestsChan:
			if !ok {
				if emitted == 0 {
					t.Fatal("expected at least one request to be emitted before the channel closed")
				}
				return
			}
			if !containsHTTPRequest(requests, r) {
				t.Fatalf("emitted request was not in the configured slice: %+v", r)
			}
			emitted++
		case <-deadline:
			t.Fatal("channel did not close within 5s of the 1s max duration")
		}
	}
}

func TestGetWarmupGrpcRequestsEmitsOnlyConfiguredRequestsAndCloses(t *testing.T) {
	requests := []mgrpc.Request{
		{ServiceMethod: "grpc.testing.TestService/EmptyCall"},
		{ServiceMethod: "grpc.testing.TestService/UnaryCall", Message: `{"payload":{"body":"abc"}}`},
	}
	w := Warmup{GrpcRequests: requests}

	requestsChan := w.GetWarmupGrpcRequests(1)
	emitted := 0
	deadline := time.After(5 * time.Second)

	for {
		select {
		case r, ok := <-requestsChan:
			if !ok {
				if emitted == 0 {
					t.Fatal("expected at least one request to be emitted before the channel closed")
				}
				return
			}
			if !containsGrpcRequest(requests, r) {
				t.Fatalf("emitted request was not in the configured slice: %+v", r)
			}
			emitted++
		case <-deadline:
			t.Fatal("channel did not close within 5s of the 1s max duration")
		}
	}
}

func TestHTTPWarmupWorkerCounts2xxAndNon2xxResponses(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(rw http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/fail":
			rw.WriteHeader(http.StatusInternalServerError)
		case "/created":
			rw.WriteHeader(http.StatusCreated)
		default:
			rw.WriteHeader(http.StatusOK)
		}
	}))
	defer server.Close()

	target := NewTarget(
		mhttp.Client{}, mgrpc.Client{},
		mhttp.NewClient(server.URL, false, 5000, mhttp.HTTP1), mgrpc.Client{},
		TargetOptions{},
	)
	w := Warmup{Target: target}

	requestsChan := make(chan mhttp.Request, 3)
	requestsChan <- mhttp.Request{Method: http.MethodGet, Path: "/ok"}
	requestsChan <- mhttp.Request{Method: http.MethodGet, Path: "/fail"}
	requestsChan <- mhttp.Request{Method: http.MethodGet, Path: "/created"}
	close(requestsChan)

	var wg sync.WaitGroup
	requestsSentCounter := 0
	wg.Add(1)
	w.HTTPWarmupWorker(&wg, requestsChan, nil, 0, &requestsSentCounter)
	wg.Wait()

	if requestsSentCounter != 3 {
		t.Fatalf("expected requestsSentCounter to count the 200, 500 and 201 responses, got %d", requestsSentCounter)
	}
}

func TestHTTPWarmupWorkerDoesNotCountTransportErrors(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(rw http.ResponseWriter, r *http.Request) {
		rw.WriteHeader(http.StatusOK)
	}))
	serverURL := server.URL
	// Close the listener so requests fail with a transport error.
	server.Close()

	target := NewTarget(
		mhttp.Client{}, mgrpc.Client{},
		mhttp.NewClient(serverURL, false, 5000, mhttp.HTTP1), mgrpc.Client{},
		TargetOptions{},
	)
	w := Warmup{Target: target}

	requestsChan := make(chan mhttp.Request, 1)
	requestsChan <- mhttp.Request{Method: http.MethodGet, Path: "/ok"}
	close(requestsChan)

	var wg sync.WaitGroup
	requestsSentCounter := 0
	wg.Add(1)
	w.HTTPWarmupWorker(&wg, requestsChan, nil, 0, &requestsSentCounter)
	wg.Wait()

	if requestsSentCounter != 0 {
		t.Fatalf("expected requestsSentCounter not to count transport errors, got %d", requestsSentCounter)
	}
}

func TestGrpcWarmupWorkerCountsResponses(t *testing.T) {
	grpcServer, grpcPort := fixture.StartGrpcTargetTestServer(fixture.NewCallStats())
	defer grpcServer.GracefulStop()

	client := mgrpc.NewClient(fmt.Sprintf("localhost:%d", grpcPort), true, 10000)
	if err := client.Connect(nil); err != nil {
		t.Fatalf("gRPC client failed to connect to the fixture server: %v", err)
	}
	defer client.Close()

	target := NewTarget(
		mhttp.Client{}, mgrpc.Client{},
		mhttp.Client{}, client,
		TargetOptions{},
	)
	w := Warmup{Target: target}

	requestsChan := make(chan mgrpc.Request, 2)
	requestsChan <- mgrpc.Request{ServiceMethod: "grpc.testing.TestService/EmptyCall"}
	requestsChan <- mgrpc.Request{ServiceMethod: "grpc.testing.TestService/UnaryCall", Message: `{"payload":{"body":"abc"}}`}
	close(requestsChan)

	var wg sync.WaitGroup
	requestsSentCounter := 0
	wg.Add(1)
	w.GrpcWarmupWorker(&wg, requestsChan, nil, 0, &requestsSentCounter)
	wg.Wait()

	// The fixture service returns Unimplemented for every method, but the RPCs are
	// still sent and counted, matching the end-to-end suite's semantics.
	if requestsSentCounter != 2 {
		t.Fatalf("expected requestsSentCounter to count both sent requests, got %d", requestsSentCounter)
	}
}

func TestGrpcWarmupWorkerDoesNotCountErrors(t *testing.T) {
	// A client that never connected returns an error from SendRequest.
	client := mgrpc.NewClient("localhost:1", true, 1000)

	target := NewTarget(
		mhttp.Client{}, mgrpc.Client{},
		mhttp.Client{}, client,
		TargetOptions{},
	)
	w := Warmup{Target: target}

	requestsChan := make(chan mgrpc.Request, 1)
	requestsChan <- mgrpc.Request{ServiceMethod: "grpc.testing.TestService/EmptyCall"}
	close(requestsChan)

	var wg sync.WaitGroup
	requestsSentCounter := 0
	wg.Add(1)
	w.GrpcWarmupWorker(&wg, requestsChan, nil, 0, &requestsSentCounter)
	wg.Wait()

	if requestsSentCounter != 0 {
		t.Fatalf("expected requestsSentCounter not to count failed requests, got %d", requestsSentCounter)
	}
}

func TestWaitForRampUpReturnsImmediately(t *testing.T) {
	start := time.Now()

	waitForRampUp(0, 1)
	waitForRampUp(0, 5)
	waitForRampUp(5, 1)

	if elapsed := time.Since(start); elapsed > 500*time.Millisecond {
		t.Fatalf("expected waitForRampUp to return immediately, took %v", elapsed)
	}
}

func TestRunSendsHTTPRequestsAndSignalsDone(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(rw http.ResponseWriter, r *http.Request) {
		rw.WriteHeader(http.StatusOK)
	}))
	defer server.Close()

	target := NewTarget(
		mhttp.Client{}, mgrpc.Client{},
		mhttp.NewClient(server.URL, false, 5000, mhttp.HTTP1), mgrpc.Client{},
		TargetOptions{},
	)
	w := Warmup{
		Target:                   target,
		Concurrency:              1,
		HttpRequests:             []mhttp.Request{{Method: http.MethodGet, Path: "/ok"}},
		RequestDelayMilliseconds: 0,
		ConcurrencyTargetSeconds: 0,
	}

	requestsSentCounter := 0
	w.Run(true, false, 1, &requestsSentCounter)

	if requestsSentCounter == 0 {
		t.Fatal("expected Run to send at least one request before the max duration elapsed")
	}
}

func TestRunConcurrentWorkersCounterRace(t *testing.T) {
	t.Skip("documents a bug: requestsSentCounter is incremented via *counter++ by concurrent " +
		"worker goroutines without synchronization, so increments can be lost and `go test -race` " +
		"reports a data race; unskip once the counter uses atomic operations or a mutex")

	server := httptest.NewServer(http.HandlerFunc(func(rw http.ResponseWriter, r *http.Request) {
		rw.WriteHeader(http.StatusOK)
	}))
	defer server.Close()

	target := NewTarget(
		mhttp.Client{}, mgrpc.Client{},
		mhttp.NewClient(server.URL, false, 5000, mhttp.HTTP1), mgrpc.Client{},
		TargetOptions{},
	)
	w := Warmup{
		Target:                   target,
		Concurrency:              4,
		HttpRequests:             []mhttp.Request{{Method: http.MethodGet, Path: "/ok"}},
		RequestDelayMilliseconds: 0,
		ConcurrencyTargetSeconds: 0,
	}

	requestsSentCounter := 0
	w.Run(true, false, 1, &requestsSentCounter)
}
