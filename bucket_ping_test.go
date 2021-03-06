package gocb

import (
	"bytes"
	"testing"
	"time"

	gocbcore "github.com/couchbase/gocbcore/v8"
	"github.com/pkg/errors"
)

func TestPingAll(t *testing.T) {
	results := map[string]gocbcore.PingResult{
		"server1": {
			Endpoint: "server1",
			Latency:  25 * time.Millisecond,
			Scope:    "default",
		},
		"server2": {
			Endpoint: "server2",
			Latency:  42 * time.Millisecond,
			Error:    errors.New("something"),
			Scope:    "default",
		},
		"server3": {
			Endpoint: "server3",
			Latency:  100 * time.Millisecond,
			Error:    gocbcore.ErrCancelled,
			Scope:    "default",
		},
	}
	pingResult := &gocbcore.PingKvResult{
		ConfigRev: 64,
		Services: []gocbcore.PingResult{
			results["server1"],
			results["server2"],
			results["server3"],
		},
	}

	doHTTP := func(req *gocbcore.HttpRequest) (*gocbcore.HttpResponse, error) {
		var endpoint string
		switch req.Service {
		case gocbcore.N1qlService:
			<-time.After(50 * time.Millisecond)
			endpoint = "http://localhost:8093"

			req.Endpoint = endpoint

			return &gocbcore.HttpResponse{
				Endpoint:   endpoint,
				StatusCode: 200,
				Body:       &testReadCloser{bytes.NewBufferString(""), nil},
			}, nil
		case gocbcore.FtsService:
			req.Endpoint = "http://localhost:8094"
			return nil, errors.New("some error occurred")
		case gocbcore.CbasService:
			<-time.After(20 * time.Millisecond)
			endpoint = "http://localhost:8095"

			req.Endpoint = endpoint

			return &gocbcore.HttpResponse{
				Endpoint:   endpoint,
				StatusCode: 200,
				Body:       &testReadCloser{bytes.NewBufferString(""), nil},
			}, nil
		default:
			return nil, errors.New("unexpected service type")
		}
	}

	kvProvider := &mockKvProvider{
		value: pingResult,
	}

	httpProvider := &mockHTTPProvider{
		doFn: doHTTP,
	}

	clients := make(map[string]client)
	cli := &mockClient{
		bucketName:        "mock",
		collectionId:      0,
		scopeId:           0,
		useMutationTokens: false,
		mockKvProvider:    kvProvider,
		mockHTTPProvider:  httpProvider,
	}
	clients["mock"] = cli
	c := &Cluster{
		connections: clients,
	}

	b := &Bucket{
		sb: stateBlock{
			clientStateBlock: clientStateBlock{
				BucketName: "mock",
			},

			KvTimeout:        c.sb.KvTimeout,
			AnalyticsTimeout: c.sb.AnalyticsTimeout,
			QueryTimeout:     c.sb.QueryTimeout,
			SearchTimeout:    c.sb.SearchTimeout,
			cachedClient:     cli,
		},
	}

	report, err := b.Ping(nil)
	if err != nil {
		t.Fatalf("Expected ping to not return error but was %v", err)
	}

	if report.ID == "" {
		t.Fatalf("Report ID was empty")
	}

	if len(report.Services) != 4 {
		t.Fatalf("Expected services length to be 6 but was %d", len(report.Services))
	}

	if report.ConfigRev != 64 {
		t.Fatalf("Expected report ConfigRev to be 64, was %d", report.ConfigRev)
	}

	for serviceType, services := range report.Services {
		for _, service := range services {
			switch serviceType {
			case QueryService:
				if service.RemoteAddr != "http://localhost:8093" {
					t.Fatalf("Expected service RemoteAddr to be http://localhost:8093 but was %s", service.RemoteAddr)
				}

				if service.State != "ok" {
					t.Fatalf("Expected service state to be ok but was %s", service.State)
				}

				if service.Latency < 50*time.Millisecond {
					t.Fatalf("Expected service latency to be over 50ms but was %d", service.Latency)
				}
			case SearchService:
				if service.RemoteAddr != "http://localhost:8094" {
					t.Fatalf("Expected service RemoteAddr to be http://localhost:8094 but was %s", service.RemoteAddr)
				}

				if service.State != "error" {
					t.Fatalf("Expected service State to be error but was %s", service.State)
				}

				if service.Latency != 0 {
					t.Fatalf("Expected service latency to be 0 but was %d", service.Latency)
				}
			case AnalyticsService:
				if service.RemoteAddr != "http://localhost:8095" {
					t.Fatalf("Expected service RemoteAddr to be http://localhost:8095 but was %s", service.RemoteAddr)
				}

				if service.State != "ok" {
					t.Fatalf("Expected service state to be ok but was %s", service.State)
				}

				if service.Latency < 20*time.Millisecond {
					t.Fatalf("Expected service latency to be over 20ms but was %d", service.Latency)
				}
			case KeyValueService:
				expected, ok := results[service.RemoteAddr]
				if !ok {
					t.Fatalf("Unexpected service endpoint: %s", service.RemoteAddr)
				}
				if service.Latency != expected.Latency {
					t.Fatalf("Expected service Latency to be %s but was %s", expected.Latency, service.Latency)
				}

				if expected.Error != nil {
					if service.State != "error" {
						t.Fatalf("Service success should have been error, was %s", service.State)
					}
				} else {
					if service.State != "ok" {
						t.Fatalf("Service success should have been ok, was %s", service.State)
					}
				}
			default:
				t.Fatalf("Unexpected service type: %d", serviceType)
			}
		}
	}
}

func TestPingTimeoutQueryOnly(t *testing.T) {
	doHTTP := func(req *gocbcore.HttpRequest) (*gocbcore.HttpResponse, error) {
		req.Endpoint = "http://localhost:8094"
		<-req.Context.Done()
		return nil, req.Context.Err()
	}

	provider := &mockHTTPProvider{
		doFn: doHTTP,
	}

	clients := make(map[string]client)
	cli := &mockClient{
		bucketName:        "mock",
		collectionId:      0,
		scopeId:           0,
		useMutationTokens: false,
		mockHTTPProvider:  provider,
	}
	clients["mock-false"] = cli
	c := &Cluster{
		connections: clients,
	}
	c.sb.QueryTimeout = 10 * time.Millisecond

	b := &Bucket{
		sb: stateBlock{
			clientStateBlock: clientStateBlock{
				BucketName: "mock",
			},

			AnalyticsTimeout: c.sb.AnalyticsTimeout,
			QueryTimeout:     c.sb.QueryTimeout,
			SearchTimeout:    c.sb.SearchTimeout,
			cachedClient:     cli,
		},
	}

	report, err := b.Ping(&PingOptions{ServiceTypes: []ServiceType{QueryService}, ReportID: "myreportid"})
	if err != nil {
		t.Fatalf("Expected ping to not return error but was %v", err)
	}

	if report.ID != "myreportid" {
		t.Fatalf("Expected report ID to be myreportid but was %s", report.ID)
	}

	if len(report.Services) != 1 {
		t.Fatalf("Expected report to have 1 service but has %d", len(report.Services))
	}

	service := report.Services[QueryService][0]
	if service.RemoteAddr != "http://localhost:8094" {
		t.Fatalf("Expected service RemoteAddr to be http://localhost:8094 but was %s", service.RemoteAddr)
	}

	if service.State != "error" {
		t.Fatalf("Expected service State to be error, was %s", service.State)
	}

	if service.Latency != 0 {
		t.Fatalf("Expected service latency to be 0 but was %d", service.Latency)
	}
}
