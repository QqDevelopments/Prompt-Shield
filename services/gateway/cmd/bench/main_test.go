package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	pb "github.com/qqdevelopments/prompt-shield/services/gateway/gen/shieldv1"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestCaseFilePreservesAllSyntheticVariants(t *testing.T) {
	path := filepath.Join(t.TempDir(), "cases.json")
	if err := os.WriteFile(path, []byte(`[{"name":"ner-32","text":"first"},{"name":"ner-32","text":"second"}]`), 0600); err != nil {
		t.Fatal(err)
	}
	cases, err := loadCases(config{scenario: "ner-32", cases: path})
	if err != nil {
		t.Fatal(err)
	}
	if len(cases) != 2 || cases[0].Text != "first" || cases[1].Text != "second" {
		t.Fatalf("variants lost: %+v", cases)
	}
}

func TestStreamingBenchmarkVerifiesRestoredTextAndTermination(t *testing.T) {
	chunk := func(content string, finish any) string {
		data, err := json.Marshal(map[string]any{"choices": []any{map[string]any{"index": 0,
			"delta": map[string]string{"content": content}, "finish_reason": finish}}})
		if err != nil {
			t.Fatal(err)
		}
		return fmt.Sprintf("data: %s\n\n", data)
	}
	stream := chunk("Éva ", nil) + chunk("Smith", "stop") + "data: [DONE]\n\n"
	if !validStream([]byte(stream), "Éva Smith") {
		t.Fatal("valid stream rejected")
	}
	if validStream([]byte(stream), "different") {
		t.Fatal("wrong content accepted")
	}
	if validStream([]byte(chunk("Éva Smith", nil)+"data: [DONE]\n\n"), "Éva Smith") {
		t.Fatal("missing finish accepted")
	}
	if validStream([]byte(stream+chunk("extra", "stop")), "Éva Smith") {
		t.Fatal("content after terminal accepted")
	}
}

func TestBenchmarkCountsExpectedDecisions(t *testing.T) {
	clean := workload{Text: "hello", Expected: "allowed"}
	if classify(clean, &pb.SanitizeResponse{SanitizedText: "hello"}, nil) != "ok" {
		t.Fatal("clean response rejected")
	}
	if classify(workload{Expected: "blocked"}, &pb.SanitizeResponse{IsBlocked: true}, nil) != "expected_block" {
		t.Fatal("block misclassified")
	}
	if classify(workload{Expected: "invalid"}, nil, status.Error(codes.InvalidArgument, "limit")) != "expected_rejection" {
		t.Fatal("limit misclassified")
	}
	if classify(clean, nil, status.Error(codes.ResourceExhausted, "busy")) != "grpc_ResourceExhausted" {
		t.Fatal("overload hidden")
	}
	if classify(workload{Text: "sensitive", Expected: "masked"}, &pb.SanitizeResponse{SanitizedText: "sensitive", EntityCount: 1}, nil) != "unexpected_decision" {
		t.Fatal("unmasked text accepted")
	}
}

func TestHTTPDeadlineIsMeasuredWithoutHidingTransportFailures(t *testing.T) {
	if got := classifyHTTPError(context.DeadlineExceeded, false); got != "http_deadline_exceeded" {
		t.Fatalf("deadline outcome = %q", got)
	}
	if !isMeasuredOutcome("http_deadline_exceeded") {
		t.Fatal("HTTP deadline capacity result rejected")
	}
	if isMeasuredOutcome("grpc_DeadlineExceeded") {
		t.Fatal("direct gRPC deadline accepted")
	}
	if got := classifyHTTPError(errors.New("connection reset"), false); got != "http_transport_error" {
		t.Fatalf("transport outcome = %q", got)
	}
	if isMeasuredOutcome("http_transport_error") || isMeasuredOutcome("body_error") {
		t.Fatal("unexpected transport failure accepted")
	}
	if got := classifyHTTPError(context.Canceled, true); got != "canceled" {
		t.Fatalf("cancellation outcome = %q", got)
	}
}

func TestAPIErrorCodeRequiresStructuredError(t *testing.T) {
	if got := apiErrorCode([]byte(`{"error":{"code":"inspection_busy"}}`)); got != "inspection_busy" {
		t.Fatalf("structured error code = %q", got)
	}
	if got := apiErrorCode([]byte(`{"message":"inspection_busy"}`)); got != "" {
		t.Fatalf("unstructured error accepted: %q", got)
	}
}

func TestHistogramIncludesTailAndEmptyIsUnavailable(t *testing.T) {
	var h histogram
	if h.quantiles()["p99_ms"] != nil {
		t.Fatal("empty latency reported as zero")
	}
	for i := 0; i < 99; i++ {
		h.add(time.Millisecond)
	}
	h.add(time.Second)
	q := h.quantiles()
	if q["p50_ms"].(float64) < 1 || q["p50_ms"].(float64) > 1.02 {
		t.Fatal(q)
	}
	h.add(time.Second)
	if h.quantiles()["p99_ms"].(float64) < 1000 {
		t.Fatal("tail hidden")
	}
}
func TestMixedWorkloadDistribution(t *testing.T) {
	cases, err := loadCases(config{scenario: "mixed"})
	if err != nil {
		t.Fatal(err)
	}
	counts := map[string]int{}
	for _, w := range cases {
		counts[w.Name]++
	}
	if counts["clean-64"] != 4 || counts["masked-64"] != 3 || counts["ner-32"] != 2 || counts["blocked-64"] != 1 {
		t.Fatal(counts)
	}
}

func TestMeasurementClockObservesRealElapsedTime(t *testing.T) {
	clock, err := newMeasurementClock()
	if err != nil {
		t.Fatal(err)
	}
	start := clock.now()
	time.Sleep(5 * time.Millisecond)
	elapsed := clock.since(start)
	if elapsed < time.Millisecond || elapsed > time.Second || clock.frequency <= 0 {
		t.Fatalf("invalid clock: %s %s", clock.name, elapsed)
	}
}

func TestMeasurementClockResolvesSubMillisecondWork(t *testing.T) {
	clock, err := newMeasurementClock()
	if err != nil {
		t.Fatal(err)
	}
	resolved := false
	for i := 0; i < 1000; i++ {
		start := clock.now()
		elapsed := clock.since(start)
		if elapsed < 0 {
			t.Fatal("counter failed")
		}
		if elapsed > 0 && elapsed < time.Millisecond {
			resolved = true
			break
		}
	}
	if !resolved {
		t.Fatal("clock cannot resolve submillisecond intervals")
	}
}
