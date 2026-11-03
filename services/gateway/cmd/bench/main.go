package main

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"math"
	"net/http"
	"net/http/httptrace"
	"os"
	"runtime"
	"sort"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	pb "github.com/qqdevelopments/prompt-shield/services/gateway/gen/shieldv1"
	"github.com/qqdevelopments/prompt-shield/services/gateway/internal/engine"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

func validStream(data []byte, expected string) bool {
	scanner := bufio.NewScanner(bytes.NewReader(data))
	scanner.Buffer(make([]byte, 4096), 2*1024*1024)
	var content strings.Builder
	done, finished := false, false
	for scanner.Scan() {
		line := scanner.Text()
		if !strings.HasPrefix(line, "data: ") {
			continue
		}
		payload := strings.TrimPrefix(line, "data: ")
		if done {
			return false
		}
		if payload == "[DONE]" {
			done = true
			continue
		}
		var chunk struct {
			Choices []struct {
				Index        int
				Delta        struct{ Content string }
				FinishReason *string `json:"finish_reason"`
			}
		}
		if json.Unmarshal([]byte(payload), &chunk) != nil || len(chunk.Choices) != 1 {
			return false
		}
		choice := chunk.Choices[0]
		if choice.Index != 0 || finished {
			return false
		}
		content.WriteString(choice.Delta.Content)
		finished = choice.FinishReason != nil
	}
	return scanner.Err() == nil && done && finished && content.String() == expected
}

type workload struct {
	Name       string `json:"name"`
	Text       string `json:"text"`
	Contextual bool   `json:"contextual"`
	Expected   string `json:"expected"`
	Tokens     int    `json:"tokens,omitempty"`
}
type histogram [4096]uint64

type measurementClock struct {
	now       func() int64
	since     func(int64) time.Duration
	name      string
	frequency int64
}

func (h *histogram) add(d time.Duration) {
	n := int(math.Ceil(math.Log1p(float64(d.Nanoseconds())/1000) / math.Log(1.01)))
	if n >= len(h) {
		n = len(h) - 1
	}
	if n < 0 {
		n = 0
	}
	h[n]++
}
func (h *histogram) quantiles() map[string]any {
	var count uint64
	for _, n := range h {
		count += n
	}
	out := map[string]any{"count": count, "relative_bucket_width": 0.01}
	for name, p := range map[string]float64{"p50_ms": .5, "p95_ms": .95, "p99_ms": .99} {
		out[name] = nil
		if count == 0 {
			continue
		}
		rank := uint64(math.Ceil(float64(count) * p))
		var cumulative uint64
		for i, n := range h {
			cumulative += n
			if cumulative >= rank {
				out[name] = math.Expm1(float64(i)*math.Log(1.01)) / 1000
				break
			}
		}
	}
	return out
}

type fixture struct {
	URL        string            `json:"url"`
	Keys       map[string]string `json:"keys"`
	StatsToken string            `json:"stats_token"`
}
type config struct {
	requests, concurrency                           int
	duration, warmup, deadline, cancelAfter         time.Duration
	cancelPercent                                   int
	scenario, transport, cases, fixtureFile, output string
	ner                                             bool
}

func apiErrorCode(data []byte) string {
	var response struct {
		Error struct {
			Code string `json:"code"`
		} `json:"error"`
	}
	if json.Unmarshal(data, &response) != nil {
		return ""
	}
	return response.Error.Code
}

func main() {
	var c config
	flag.IntVar(&c.requests, "requests", 1000, "measured requests when duration is zero")
	flag.IntVar(&c.concurrency, "concurrency", 1, "concurrent closed-loop workers")
	flag.DurationVar(&c.duration, "duration", 0, "measurement duration; overrides requests")
	flag.DurationVar(&c.warmup, "warmup", 0, "warm-up duration; zero uses 20 requests")
	flag.DurationVar(&c.deadline, "deadline", time.Second, "per-request deadline")
	flag.DurationVar(&c.cancelAfter, "cancel-after", time.Millisecond, "delay after dispatch before cancellation")
	flag.IntVar(&c.cancelPercent, "cancel-percent", 0, "percentage of requests selected for cancellation")
	flag.StringVar(&c.scenario, "scenario", "", "case name or mixed")
	flag.StringVar(&c.transport, "transport", "grpc", "grpc, http, or sse")
	flag.StringVar(&c.cases, "cases", "", "tokenizer-verified workload JSON")
	flag.StringVar(&c.fixtureFile, "fixture", "", "isolated HTTP fixture connection file")
	flag.StringVar(&c.output, "output", "", "JSON report destination; default stdout")
	flag.BoolVar(&c.ner, "ner", false, "legacy short NER scenario")
	flag.Parse()
	if err := run(c); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}
func loadCases(c config) ([]workload, error) {
	cases := []workload{
		{"masked-64", "Contact alice@example.com about 4111 1111 1111 1111.", false, "masked", 0},
		{"clean-64", "Explain a rainbow.", false, "allowed", 0},
		{"blocked-64", "sk-abcdefghijklmnopqrstuvwx", false, "blocked", 0},
		{"ner-32", "John Smith works at Microsoft in London.", true, "masked", 0},
	}
	if c.cases != "" {
		data, err := os.ReadFile(c.cases)
		if err != nil {
			return nil, err
		}
		if err = json.Unmarshal(data, &cases); err != nil {
			return nil, err
		}
	}
	wanted := c.scenario
	if wanted == "" {
		wanted = "masked-64"
		if c.ner {
			wanted = "ner-32"
		}
	}
	if wanted == "mixed" || wanted == "mixed-long" {
		order := []string{"clean-64", "clean-64", "clean-64", "clean-64", "masked-64", "masked-64", "masked-64", "ner-32", "ner-32", "blocked-64"}
		if wanted == "mixed-long" {
			order[7], order[8] = "ner-128", "ner-512"
		}
		selected := []workload{}
		// Expand complete mix cycles to include every variant while preserving
		// the 40/30/20/10 policy distribution.
		variants := map[string][]workload{}
		cycles := 1
		for _, w := range cases {
			variants[w.Name] = append(variants[w.Name], w)
			if len(variants[w.Name]) > cycles {
				cycles = len(variants[w.Name])
			}
		}
		for cycle := 0; cycle < cycles; cycle++ {
			for _, name := range order {
				found := false
				if choices := variants[name]; len(choices) > 0 {
					selected = append(selected, choices[cycle%len(choices)])
					found = true
				}
				if !found {
					return nil, fmt.Errorf("missing workload %s", name)
				}
			}
		}
		return selected, nil
	}
	var selected []workload
	for _, w := range cases {
		if w.Name == wanted {
			selected = append(selected, w)
		}
	}
	if len(selected) > 0 {
		return selected, nil
	}
	return nil, fmt.Errorf("unknown scenario %s", wanted)
}
func classify(w workload, result *pb.SanitizeResponse, err error) string {
	if err != nil {
		if w.Expected == "invalid" && status.Code(err) == codes.InvalidArgument {
			return "expected_rejection"
		}
		return "grpc_" + status.Code(err).String()
	}
	if result == nil {
		return "invalid_response"
	}
	switch w.Expected {
	case "allowed":
		if !result.IsBlocked && result.EntityCount == 0 && result.SanitizedText == w.Text {
			return "ok"
		}
	case "masked":
		if !result.IsBlocked && result.EntityCount > 0 && result.SanitizedText != w.Text {
			return "ok"
		}
	case "blocked":
		if result.IsBlocked {
			return "expected_block"
		}
	}
	return "unexpected_decision"
}

func classifyHTTPError(err error, cancellationSignaled bool) string {
	if cancellationSignaled && errors.Is(err, context.Canceled) {
		return "canceled"
	}
	if errors.Is(err, context.DeadlineExceeded) {
		return "http_deadline_exceeded"
	}
	return "http_transport_error"
}

func isMeasuredOutcome(outcome string) bool {
	switch outcome {
	case "ok", "expected_block", "expected_rejection", "canceled",
		"grpc_ResourceExhausted",
		"http_503_capacity", "http_429", "http_deadline_exceeded":
		return true
	default:
		return false
	}
}

func run(c config) error {
	clock, err := newMeasurementClock()
	if err != nil {
		return err
	}
	if c.requests < 1 || c.concurrency < 1 || c.concurrency > 64 || c.duration < 0 || c.warmup < 0 ||
		c.deadline <= 0 || c.cancelAfter <= 0 || c.cancelPercent < 0 || c.cancelPercent > 100 {
		return errors.New("invalid benchmark configuration")
	}
	if c.transport != "grpc" && c.transport != "http" && c.transport != "sse" {
		return errors.New("invalid transport")
	}
	cases, err := loadCases(c)
	if err != nil {
		return err
	}
	client, err := engine.New(os.Getenv("RUST_ENGINE_ADDR"), os.Getenv("ENGINE_TOKEN"))
	if err != nil {
		return err
	}
	defer client.Close()
	ready, done := context.WithTimeout(context.Background(), 5*time.Second)
	err = client.Ready(ready)
	done()
	if err != nil {
		return errors.New("engine preflight failed")
	}
	var f fixture
	hc := &http.Client{Timeout: c.deadline, Transport: &http.Transport{MaxIdleConns: 128, MaxIdleConnsPerHost: 64}}
	defer hc.CloseIdleConnections()
	if c.transport != "grpc" {
		data, e := os.ReadFile(c.fixtureFile)
		if e != nil {
			return e
		}
		if e = json.Unmarshal(data, &f); e != nil {
			return e
		}
		if !strings.HasPrefix(f.URL, "http://127.0.0.1:") {
			return errors.New("fixture must be loopback")
		}
	}
	invoke := func(w workload, cancelRequest bool) (string, time.Duration, bool) {
		ctx, cancel := context.WithTimeout(context.Background(), c.deadline)
		defer cancel()
		var canceled atomic.Bool
		var timerMu sync.Mutex
		var timer *time.Timer
		arm := func() {
			if cancelRequest {
				timerMu.Lock()
				timer = time.AfterFunc(c.cancelAfter, func() { canceled.Store(true); cancel() })
				timerMu.Unlock()
			}
		}
		defer func() {
			timerMu.Lock()
			if timer != nil {
				timer.Stop()
			}
			timerMu.Unlock()
		}()
		before := clock.now()
		if c.transport == "grpc" {
			arm()
			result, e := client.Inspect(ctx, &pb.SanitizeRequest{RequestId: "load-probe", OrganizationId: "load-fixture", Text: w.Text,
				Policy: &pb.SecurityPolicy{MaskPii: true, BlockSecrets: true, ContextualPii: w.Contextual}})
			if canceled.Load() && status.Code(e) == codes.Canceled {
				return "canceled", clock.since(before), true
			}
			return classify(w, result, e), clock.since(before), canceled.Load()
		}
		body, _ := json.Marshal(map[string]any{"model": "fixture-model", "max_tokens": 16, "stream": c.transport == "sse",
			"messages": []map[string]string{{"role": "user", "content": w.Text}}})
		ctx = httptrace.WithClientTrace(ctx, &httptrace.ClientTrace{WroteRequest: func(httptrace.WroteRequestInfo) { arm() }})
		req, e := http.NewRequestWithContext(ctx, http.MethodPost, f.URL+"/v1/chat/completions", bytes.NewReader(body))
		if e != nil {
			return "request_error", clock.since(before), false
		}
		key := "rules"
		if w.Contextual {
			key = "ner"
		}
		req.Header.Set("Authorization", "Bearer "+f.Keys[key])
		req.Header.Set("Content-Type", "application/json")
		resp, e := hc.Do(req)
		if e != nil {
			return classifyHTTPError(e, canceled.Load()), clock.since(before), canceled.Load()
		}
		defer resp.Body.Close()
		data, e := io.ReadAll(io.LimitReader(resp.Body, 2*1024*1024))
		if e != nil {
			outcome := classifyHTTPError(e, canceled.Load())
			if outcome != "http_transport_error" {
				return outcome, clock.since(before), canceled.Load()
			}
			return "body_error", clock.since(before), canceled.Load()
		}
		outcome := fmt.Sprintf("http_%d", resp.StatusCode)
		failureCode := apiErrorCode(data)
		if w.Expected == "blocked" && resp.StatusCode == 400 && bytes.Contains(data, []byte("prompt_shield_blocked")) {
			outcome = "expected_block"
		}
		if w.Expected == "invalid" && resp.StatusCode == 503 && failureCode == "inspection_unavailable" {
			outcome = "expected_rejection"
		}
		if resp.StatusCode == 503 && failureCode == "inspection_busy" {
			outcome = "http_503_capacity"
		}
		if resp.StatusCode == 200 {
			if c.transport == "sse" {
				if validStream(data, w.Text) {
					outcome = "ok"
				} else {
					outcome = "invalid_stream"
				}
			} else {
				var response struct {
					Choices []struct{ Message struct{ Content string } }
				}
				if json.Unmarshal(data, &response) == nil && len(response.Choices) == 1 && response.Choices[0].Message.Content == w.Text {
					outcome = "ok"
				} else {
					outcome = "invalid_restoration"
				}
			}
		}
		return outcome, clock.since(before), canceled.Load()
	}
	// Warm-up results are checked, not silently ignored.
	warmStart := time.Now()
	for i := 0; (c.warmup == 0 && i < 20) || (c.warmup > 0 && time.Since(warmStart) < c.warmup); i++ {
		result, _, _ := invoke(cases[i%len(cases)], false)
		if result != "ok" && result != "expected_block" && result != "expected_rejection" && result != "http_429" {
			return fmt.Errorf("warm-up failed: %s", result)
		}
	}
	warmupSeconds := time.Since(warmStart).Seconds()
	var mu sync.Mutex
	var all, success, cancellation histogram
	outcomes, mix := map[string]int64{}, map[string]int64{}
	var index atomic.Int64
	var wg sync.WaitGroup
	var selected, cancelSignals int64
	start := time.Now()
	for worker := 0; worker < c.concurrency; worker++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for {
				i := index.Add(1) - 1
				if c.duration > 0 {
					if time.Since(start) >= c.duration {
						return
					}
				} else if i >= int64(c.requests) {
					return
				}
				w := cases[i%int64(len(cases))]
				// Rotate the selected positions to avoid coupling cancellation to mixed-case order.
				cancelRequest := (i*37+17)%100 < int64(c.cancelPercent)
				result, elapsed, signaled := invoke(w, cancelRequest)
				if elapsed <= 0 {
					result = "clock_resolution_error"
				}
				mu.Lock()
				all.add(elapsed)
				outcomes[result]++
				mix[w.Name]++
				if cancelRequest {
					selected++
				}
				if signaled {
					cancelSignals++
				}
				if result == "canceled" {
					cancellation.add(elapsed)
				}
				if result == "ok" || result == "expected_block" || result == "expected_rejection" {
					success.add(elapsed)
				}
				mu.Unlock()
			}
		}()
	}
	wg.Wait()
	duration := time.Since(start).Seconds()
	var total, good int64
	for key, n := range outcomes {
		total += n
		if key == "ok" || key == "expected_block" || key == "expected_rejection" {
			good += n
		}
	}
	// Probe the engine independently of HTTP rate-limit state after draining client calls.
	recoveryStart := time.Now()
	recovered := false
	for time.Since(recoveryStart) < 10*time.Second {
		probe, stop := context.WithTimeout(context.Background(), time.Second)
		e := client.Ready(probe)
		stop()
		if e == nil {
			recovered = true
			break
		}
		time.Sleep(25 * time.Millisecond)
	}
	inputs := []map[string]any{}
	seen := map[string]bool{}
	for _, w := range cases {
		if !seen[w.Name] {
			inputs = append(inputs, map[string]any{"name": w.Name, "bytes": len(w.Text), "tokens": w.Tokens, "contextual": w.Contextual, "expected": w.Expected})
			seen[w.Name] = true
		}
	}
	sort.Slice(inputs, func(i, j int) bool { return inputs[i]["name"].(string) < inputs[j]["name"].(string) })
	report := map[string]any{"schema_version": 1, "transport": c.transport, "scenario": c.scenario, "inputs": inputs,
		"measurement_clock": clock.name, "clock_frequency_hz": clock.frequency,
		"load_model": "closed_loop", "concurrency": c.concurrency, "warmup_seconds": warmupSeconds,
		"duration_seconds": duration, "deadline_ms": c.deadline.Milliseconds(), "requests": total, "successful_requests": good,
		"outcomes": outcomes, "actual_mix": mix, "attempted_rps": float64(total) / duration, "successful_rps": float64(good) / duration,
		"error_rate": float64(total-good-outcomes["canceled"]) / float64(total), "unsuccessful_rate": float64(total-good) / float64(total),
		"all_latency": all.quantiles(), "successful_latency": success.quantiles(), "cancellation_latency": cancellation.quantiles(),
		"cancellation_selected": selected, "cancellation_signaled": cancelSignals, "cancel_after_ms": float64(c.cancelAfter) / float64(time.Millisecond),
		"recovered": recovered, "recovery_ms": float64(time.Since(recoveryStart)) / float64(time.Millisecond),
		"go_version": runtime.Version(), "os": runtime.GOOS, "architecture": runtime.GOARCH, "logical_cpus": runtime.NumCPU(),
		"notes": []string{"Latency histograms use 1% logarithmic buckets.", "Cancellation latency is dispatch-to-return; a timer race may complete normally.", "HTTP fixture preserves production 120 requests/minute per organization/key.", "Engine recovery is observable; RPC cancellation does not prove native inference preemption."}}
	var out io.Writer = os.Stdout
	if c.output != "" {
		file, e := os.Create(c.output)
		if e != nil {
			return e
		}
		defer file.Close()
		out = file
	}
	if err = json.NewEncoder(out).Encode(report); err != nil {
		return err
	}
	if !recovered {
		return errors.New("engine failed recovery probes")
	}
	// Deadline exhaustion, explicit capacity responses and 429 remain unsuccessful measurements.
	// Unknown transport/protocol outcomes still fail the condition.
	for key := range outcomes {
		if !isMeasuredOutcome(key) {
			return fmt.Errorf("unexpected outcome: %s", key)
		}
	}
	return nil
}
