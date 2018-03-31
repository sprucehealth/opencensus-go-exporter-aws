// Copyright 2018, OpenCensus Authors
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
//     http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.

package aws_test

import (
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/aws/aws-sdk-go/service/xray"
	"github.com/aws/aws-sdk-go/service/xray/xrayiface"
	"github.com/census-instrumentation/opencensus-go-exporter-aws"
	"go.opencensus.io/plugin/ochttp"
	"go.opencensus.io/trace"
)

type mockSegments struct {
	xrayiface.XRayAPI
	ch chan string
}

func (m *mockSegments) PutTraceSegments(in *xray.PutTraceSegmentsInput) (*xray.PutTraceSegmentsOutput, error) {
	for _, doc := range in.TraceSegmentDocuments {
		m.ch <- *doc
	}
	return nil, nil
}

func handle(name string) http.HandlerFunc {
	return func(w http.ResponseWriter, req *http.Request) {
		w.Header().Set("Content-Type", "text/plain")
		w.Header().Set("Content-Length", "2")
		w.WriteHeader(http.StatusOK)
		io.WriteString(w, "ok")
	}
}

func TestHttp(t *testing.T) {
	const (
		userAgent = "blah-agent"
		host      = "www.example.com"
		path      = "/index"
	)

	var (
		api         = &mockSegments{ch: make(chan string, 1)}
		exporter, _ = aws.NewExporter(aws.WithAPI(api), aws.WithBufferSize(1))
	)

	trace.RegisterExporter(exporter)
	trace.SetDefaultSampler(trace.AlwaysSample())

	var h = &ochttp.Handler{
		Propagation: &aws.HTTPFormat{},
		Handler:     handle("web"),
	}

	traceID := trace.TraceID{1, 2, 3, 4, 5, 6, 7, 8, 9, 10, 11, 12, 13, 14, 15, 16}
	amazonTraceID := aws.ConvertToAmazonTraceID(traceID)
	req, _ := http.NewRequest(http.MethodGet, "http://"+host+path, strings.NewReader("hello"))

	w := httptest.NewRecorder()
	req.Header.Set(`X-Amzn-Trace-Id`, amazonTraceID)
	req.Header.Set(`User-Agent`, userAgent)

	h.ServeHTTP(w, req)

	var content struct {
		Name string
		Http struct {
			Request struct {
				Method    string
				URL       string `json:"url"`
				UserAgent string `json:"user_agent"`
			}
		}
	}

	v := <-api.ch
	if err := json.Unmarshal([]byte(v), &content); err != nil {
		t.Fatalf("unable to decode content, %v", err)
	}

	if want := host; want != content.Name {
		t.Errorf("want %v; got %v", want, content.Name)
	}
	if want := http.MethodGet; want != content.Http.Request.Method {
		t.Errorf("want %v; got %v", want, content.Http.Request.Method)
	}
	if want := userAgent; want != content.Http.Request.UserAgent {
		t.Errorf("want %v; got %v", want, content.Http.Request.UserAgent)
	}
	if want := host + path; want != content.Http.Request.URL {
		t.Errorf("want %v; got %v", want, content.Http.Request.URL)
	}
}
