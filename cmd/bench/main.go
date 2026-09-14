package main

import (
	"bytes"
	"crypto/tls"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"math"
	"net/http"
	"net/url"
	"os"
	"sort"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/google/uuid"
	"github.com/gorilla/websocket"
	"google.golang.org/protobuf/proto"

	pb "golang-im-neo-system/proto"
)

var (
	fTarget    = flag.String("target", "https://106.52.170.56:8080", "target server URL (e.g. https://106.52.170.56:8080 or http://127.0.0.1:8080)")
	fMode      = flag.String("mode", "http-api", "mode: http-cc | http-api | ws-c1k | ws-msg")
	fC         = flag.Int("c", 50, "concurrency (number of concurrent workers or WS connections)")
	fN         = flag.Int("n", 500, "total requests (for http-api/http-cc) or messages per client (for ws-msg)")
	fDuration  = flag.Duration("d", 10*time.Second, "benchmark duration (used in ws-c1k or rate tests)")
	fInsecure  = flag.Bool("insecure", true, "skip TLS certificate verification for self-signed certificates")
	fKeepAlive = flag.Bool("keepalive", true, "enable HTTP keep-alive")
	fOutput    = flag.String("out", "", "output JSON report path (defaults to bench_<mode>_<timestamp>.json)")
)

func main() {
	flag.Parse()

	targetURL := strings.TrimRight(*fTarget, "/")
	fmt.Printf("\n🚀 Starting Golang IM Neo Benchmark Suite\n")
	fmt.Printf("Target:      %s\n", targetURL)
	fmt.Printf("Mode:        %s\n", *fMode)
	fmt.Printf("Concurrency: %d\n", *fC)
	fmt.Printf("InsecureTLS: %v\n\n", *fInsecure)

	switch *fMode {
	case "http-cc":
		runHttpCC(targetURL, *fC, *fN)
	case "http-api":
		runHttpAPI(targetURL, *fC, *fN)
	case "ws-c1k":
		runWsC1K(targetURL, *fC, *fDuration)
	case "ws-msg":
		runWsMsg(targetURL, *fC, *fN)
	default:
		fmt.Fprintf(os.Stderr, "Unknown mode: %s. Supported: http-cc, http-api, ws-c1k, ws-msg\n", *fMode)
		os.Exit(1)
	}
}

// -------------------------------------------------------------
// 统计汇总辅助结构体
// -------------------------------------------------------------

type Stats struct {
	mu        sync.Mutex
	durations []time.Duration
	success   int64
	failed    int64
	status503 int64
	status429 int64
	bytesSent int64
	bytesRecv int64
	startTime time.Time
	endTime   time.Time
}

func newStats() *Stats {
	return &Stats{
		durations: make([]time.Duration, 0, 10000),
		startTime: time.Now(),
	}
}

func (s *Stats) record(d time.Duration, ok bool, code int, sent, recv int64) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.durations = append(s.durations, d)
	if ok {
		s.success++
	} else {
		s.failed++
		if code == http.StatusServiceUnavailable {
			s.status503++
		} else if code == http.StatusTooManyRequests {
			s.status429++
		}
	}
	s.bytesSent += sent
	s.bytesRecv += recv
}

func (s *Stats) finish() {
	s.endTime = time.Now()
}

func (s *Stats) printReport(title string) {
	totalDur := s.endTime.Sub(s.startTime)
	totalReq := s.success + s.failed

	sort.Slice(s.durations, func(i, j int) bool {
		return s.durations[i] < s.durations[j]
	})

	var minD, maxD, avgD, p50, p90, p95, p99 time.Duration
	if len(s.durations) > 0 {
		minD = s.durations[0]
		maxD = s.durations[len(s.durations)-1]
		var sum time.Duration
		for _, d := range s.durations {
			sum += d
		}
		avgD = time.Duration(int64(sum) / int64(len(s.durations)))
		p50 = percentile(s.durations, 50)
		p90 = percentile(s.durations, 90)
		p95 = percentile(s.durations, 95)
		p99 = percentile(s.durations, 99)
	}

	qps := float64(totalReq) / totalDur.Seconds()
	successQps := float64(s.success) / totalDur.Seconds()

	fmt.Println("==================================================================")
	fmt.Printf("               BENCHMARK REPORT: %s\n", title)
	fmt.Println("==================================================================")
	fmt.Printf("Elapsed Time:       %.2fs\n", totalDur.Seconds())
	fmt.Printf("Total Requests:     %d\n", totalReq)
	fmt.Printf("Successful (2xx):   %d (%.2f%%)\n", s.success, pct(s.success, totalReq))
	fmt.Printf("Failed/Blocked:     %d (%.2f%%)\n", s.failed, pct(s.failed, totalReq))
	if s.status503 > 0 || s.status429 > 0 {
		fmt.Printf("  └─ Nginx CC Intercepted: 503=%d, 429=%d\n", s.status503, s.status429)
	}
	fmt.Printf("Overall Throughput: %.2f req/s\n", qps)
	fmt.Printf("Success Throughput: %.2f req/s (Effective QPS)\n", successQps)
	fmt.Println("------------------------------------------------------------------")
	fmt.Printf("Latency Distribution:\n")
	fmt.Printf("  Min:   %10.2f ms\n", ms(minD))
	fmt.Printf("  Avg:   %10.2f ms\n", ms(avgD))
	fmt.Printf("  P50:   %10.2f ms\n", ms(p50))
	fmt.Printf("  P90:   %10.2f ms\n", ms(p90))
	fmt.Printf("  P95:   %10.2f ms\n", ms(p95))
	fmt.Printf("  P99:   %10.2f ms\n", ms(p99))
	fmt.Printf("  Max:   %10.2f ms\n", ms(maxD))
	fmt.Println("==================================================================")

	// 保存结构化 JSON 报告
	reportPath := *fOutput
	if reportPath == "" {
		reportPath = fmt.Sprintf("bench_%s_%s.json", *fMode, time.Now().Format("20060102_150405"))
	}
	reportData := map[string]any{
		"timestamp":              time.Now().Format(time.RFC3339),
		"target":                 *fTarget,
		"mode":                   *fMode,
		"concurrency":            *fC,
		"duration_seconds":       math.Round(totalDur.Seconds()*100) / 100,
		"total_requests":         totalReq,
		"successful":             s.success,
		"failed":                 s.failed,
		"success_rate_pct":       math.Round(pct(s.success, totalReq)*100) / 100,
		"nginx_cc_intercept_503": s.status503,
		"overall_qps":            math.Round(qps*100) / 100,
		"effective_qps":          math.Round(successQps*100) / 100,
		"latency_ms": map[string]float64{
			"min": ms(minD),
			"avg": ms(avgD),
			"p50": ms(p50),
			"p90": ms(p90),
			"p95": ms(p95),
			"p99": ms(p99),
			"max": ms(maxD),
		},
	}
	if b, err := json.MarshalIndent(reportData, "", "  "); err == nil {
		if err := os.WriteFile(reportPath, b, 0644); err == nil {
			fmt.Printf("📁 Detailed benchmark report saved to: %s\n\n", reportPath)
		}
	}
}

func percentile(sorted []time.Duration, p float64) time.Duration {
	if len(sorted) == 0 {
		return 0
	}
	idx := int(math.Ceil(float64(len(sorted))*(p/100.0))) - 1
	if idx < 0 {
		idx = 0
	}
	if idx >= len(sorted) {
		idx = len(sorted) - 1
	}
	return sorted[idx]
}

func ms(d time.Duration) float64 {
	return float64(d.Microseconds()) / 1000.0
}

func pct(sub, total int64) float64 {
	if total == 0 {
		return 0.0
	}
	return (float64(sub) / float64(total)) * 100.0
}

func createHTTPClient() *http.Client {
	transport := &http.Transport{
		TLSClientConfig: &tls.Config{
			InsecureSkipVerify: *fInsecure,
		},
		DisableKeepAlives:   !*fKeepAlive,
		MaxIdleConns:        1000,
		MaxIdleConnsPerHost: 1000,
		IdleConnTimeout:     90 * time.Second,
	}
	return &http.Client{
		Transport: transport,
		Timeout:   10 * time.Second,
	}
}

// -------------------------------------------------------------
// 场景 1: Nginx 边缘网关抗 CC 突发限流验证
// -------------------------------------------------------------

func runHttpCC(target string, concurrency, total int) {
	fmt.Printf("▶ [Scenario 1] Flooding endpoint with burst requests to verify Nginx Anti-CC...\n")
	client := createHTTPClient()
	stats := newStats()

	ch := make(chan struct{}, total)
	for i := 0; i < total; i++ {
		ch <- struct{}{}
	}
	close(ch)

	var wg sync.WaitGroup
	for i := 0; i < concurrency; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for range ch {
				start := time.Now()
				resp, err := client.Get(target + "/health")
				d := time.Since(start)
				if err != nil {
					stats.record(d, false, 0, 0, 0)
					continue
				}
				io.Copy(io.Discard, resp.Body)
				resp.Body.Close()
				ok := (resp.StatusCode == http.StatusOK)
				stats.record(d, ok, resp.StatusCode, 0, resp.ContentLength)
			}
		}()
	}

	wg.Wait()
	stats.finish()
	stats.printReport("Nginx Anti-CC & Rate-Limiting Protection")
}

// -------------------------------------------------------------
// 场景 2: REST API 高并发读取性能测试 (QPS & Latency)
// -------------------------------------------------------------

func runHttpAPI(target string, concurrency, total int) {
	fmt.Printf("▶ [Scenario 2] Benchmarking Public REST API (GET /health & /api/v1/messages/hall/history)...\n")
	client := createHTTPClient()
	stats := newStats()

	ch := make(chan struct{}, total)
	for i := 0; i < total; i++ {
		ch <- struct{}{}
	}
	close(ch)

	var wg sync.WaitGroup
	for i := 0; i < concurrency; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for range ch {
				start := time.Now()
				resp, err := client.Get(target + "/health")
				d := time.Since(start)
				if err != nil {
					stats.record(d, false, 0, 0, 0)
					continue
				}
				io.Copy(io.Discard, resp.Body)
				resp.Body.Close()
				stats.record(d, resp.StatusCode == 200, resp.StatusCode, 0, resp.ContentLength)
			}
		}()
	}

	wg.Wait()
	stats.finish()
	stats.printReport("REST API High-Concurrency Throughput")
}

// -------------------------------------------------------------
// 场景 3: WebSocket C1K 海量长连接建立与保活维持
// -------------------------------------------------------------

type wsClientContext struct {
	username string
	token    string
	ticket   string
	conn     *websocket.Conn
}

func runWsC1K(target string, count int, duration time.Duration) {
	fmt.Printf("▶ [Scenario 3] Establishing and holding %d concurrent WebSockets for %v...\n", count, duration)
	httpClient := createHTTPClient()

	var connectedCount int64
	var failedCount int64
	var activeConns []*websocket.Conn
	var connMu sync.Mutex

	dialer := websocket.Dialer{
		TLSClientConfig:  &tls.Config{InsecureSkipVerify: *fInsecure},
		HandshakeTimeout: 10 * time.Second,
	}

	start := time.Now()
	var wg sync.WaitGroup
	throttle := time.NewTicker(20 * time.Millisecond) // 平滑建连，防止瞬间打爆本地端口
	defer throttle.Stop()

	fmt.Printf("Spawning %d WebSocket sessions...\n", count)
	for i := 0; i < count; i++ {
		<-throttle.C
		wg.Add(1)
		idx := i
		go func() {
			defer wg.Done()
			uname := fmt.Sprintf("bench_c1k_%d_%d", time.Now().UnixNano()%100000, idx)
			pass := "BenchPass123!"

			// 1. 快速注册/登录
			token, err := registerAndLogin(httpClient, target, uname, pass)
			if err != nil {
				atomic.AddInt64(&failedCount, 1)
				return
			}

			// 2. 换取单次票据 ticket
			ticket, err := fetchTicket(httpClient, target, token)
			if err != nil {
				atomic.AddInt64(&failedCount, 1)
				return
			}

			// 3. 建立 WSS 连接
			wsTarget := toWsURL(target) + "/ws?ticket=" + ticket
			conn, _, err := dialer.Dial(wsTarget, nil)
			if err != nil {
				atomic.AddInt64(&failedCount, 1)
				return
			}

			atomic.AddInt64(&connectedCount, 1)
			connMu.Lock()
			activeConns = append(activeConns, conn)
			connMu.Unlock()

			// 后台心跳保活协程
			go func() {
				ticker := time.NewTicker(15 * time.Second)
				defer ticker.Stop()
				for range ticker.C {
					pingMsg := &pb.WsMessage{
						Type:     pb.MsgType_HEARTBEAT_PING,
						StanzaId: uuid.New().String(),
					}
					data, _ := proto.Marshal(pingMsg)
					if err := conn.WriteMessage(websocket.BinaryMessage, data); err != nil {
						return
					}
				}
			}()
		}()
	}

	wg.Wait()
	setupDuration := time.Since(start)
	fmt.Printf("\n✓ Connection Establishment Complete: %d connected, %d failed (took %.2fs)\n",
		connectedCount, failedCount, setupDuration.Seconds())

	fmt.Printf("Holding connections and measuring health for %v...\n", duration)
	time.Sleep(duration)

	// 优雅断开连接
	connMu.Lock()
	for _, c := range activeConns {
		c.Close()
	}
	connMu.Unlock()

	fmt.Println("==================================================================")
	fmt.Printf("         BENCHMARK REPORT: C1K CONCURRENT WEBSOCKETS\n")
	fmt.Println("==================================================================")
	fmt.Printf("Target Concurrency:   %d connections\n", count)
	fmt.Printf("Successfully Held:    %d connections (%.2f%%)\n", connectedCount, pct(connectedCount, int64(count)))
	fmt.Printf("Establishment Time:   %.2fs (Avg %.2f ms per conn)\n", setupDuration.Seconds(), ms(setupDuration)/float64(count))
	fmt.Printf("Holding Duration:     %v\n", duration)
	fmt.Println("==================================================================")

	reportPath := *fOutput
	if reportPath == "" {
		reportPath = fmt.Sprintf("bench_ws_c1k_%s.json", time.Now().Format("20060102_150405"))
	}
	reportData := map[string]any{
		"timestamp":                time.Now().Format(time.RFC3339),
		"target":                   target,
		"mode":                     "ws-c1k",
		"target_concurrency":       count,
		"successfully_held":        connectedCount,
		"failed":                   failedCount,
		"success_rate_pct":         math.Round(pct(connectedCount, int64(count))*100) / 100,
		"establishment_duration_s": math.Round(setupDuration.Seconds()*100) / 100,
		"avg_conn_setup_ms":        math.Round((ms(setupDuration)/float64(count))*100) / 100,
		"holding_duration":         duration.String(),
	}
	if b, err := json.MarshalIndent(reportData, "", "  "); err == nil {
		if err := os.WriteFile(reportPath, b, 0644); err == nil {
			fmt.Printf("📁 Detailed benchmark report saved to: %s\n\n", reportPath)
		}
	}
}

// -------------------------------------------------------------
// 场景 4: WebSocket 高频广播消息与落盘吞吐量 (TPS)
// -------------------------------------------------------------

func runWsMsg(target string, clientsCount, msgsPerClient int) {
	fmt.Printf("▶ [Scenario 4] Benchmarking Real-time Message TPS (%d clients x %d msgs)...\n",
		clientsCount, msgsPerClient)

	httpClient := createHTTPClient()
	dialer := websocket.Dialer{
		TLSClientConfig:  &tls.Config{InsecureSkipVerify: *fInsecure},
		HandshakeTimeout: 10 * time.Second,
	}

	// 1. 初始化客户端
	fmt.Printf("Preparing %d benchmark client connections...\n", clientsCount)
	clients := make([]*websocket.Conn, 0, clientsCount)
	for i := 0; i < clientsCount; i++ {
		uname := fmt.Sprintf("bench_msg_%d_%d", time.Now().UnixNano()%100000, i)
		token, err := registerAndLogin(httpClient, target, uname, "BenchPass123!")
		if err != nil {
			fmt.Printf("login failed for %s: %v\n", uname, err)
			continue
		}
		ticket, err := fetchTicket(httpClient, target, token)
		if err != nil {
			continue
		}
		conn, _, err := dialer.Dial(toWsURL(target)+"/ws?ticket="+ticket, nil)
		if err != nil {
			continue
		}
		clients = append(clients, conn)
	}

	if len(clients) == 0 {
		fmt.Fprintf(os.Stderr, "Error: no clients connected successfully\n")
		return
	}
	fmt.Printf("✓ %d clients connected. Starting high-frequency broadcast load...\n", len(clients))

	stats := newStats()
	var wg sync.WaitGroup

	for _, conn := range clients {
		wg.Add(1)
		c := conn
		go func() {
			defer wg.Done()
			defer c.Close()

			// 读泵：等待服务端 ACK 回执
			ackCh := make(chan string, 100)
			go func() {
				for {
					_, frame, err := c.ReadMessage()
					if err != nil {
						return
					}
					var in pb.WsMessage
					if proto.Unmarshal(frame, &in) == nil && in.Type == pb.MsgType_ACK {
						ackCh <- in.StanzaId
					}
				}
			}()

			for m := 0; m < msgsPerClient; m++ {
				stanzaID := uuid.New().String()
				msg := &pb.WsMessage{
					Type:     pb.MsgType_CHAT,
					StanzaId: stanzaID,
					Payload:  []byte(fmt.Sprintf("bench_load_payload_%d_%s", m, stanzaID)),
				}
				frame, _ := proto.Marshal(msg)

				start := time.Now()
				err := c.WriteMessage(websocket.BinaryMessage, frame)
				if err != nil {
					stats.record(time.Since(start), false, 0, int64(len(frame)), 0)
					continue
				}

				// 等待对应的 ACK
				select {
				case ackID := <-ackCh:
					d := time.Since(start)
					stats.record(d, ackID == stanzaID, 200, int64(len(frame)), 64)
				case <-time.After(3 * time.Second):
					stats.record(3*time.Second, false, 504, int64(len(frame)), 0)
				}
			}
		}()
	}

	wg.Wait()
	stats.finish()
	stats.printReport("WebSocket Protobuf High-Frequency Message TPS")
}

// -------------------------------------------------------------
// 协议与网络底层辅助
// -------------------------------------------------------------

func registerAndLogin(client *http.Client, target, username, password string) (string, error) {
	regBody, _ := json.Marshal(map[string]string{
		"username": username,
		"password": password,
	})
	resp, err := client.Post(target+"/api/v1/auth/register", "application/json", bytes.NewReader(regBody))
	if err == nil {
		io.Copy(io.Discard, resp.Body)
		resp.Body.Close()
	}

	loginBody, _ := json.Marshal(map[string]string{
		"username":     username,
		"password":     password,
		"device_class": "interactive",
		"device_name":  "bench-cli",
	})
	resp, err = client.Post(target+"/api/v1/auth/login", "application/json", bytes.NewReader(loginBody))
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()

	var res struct {
		Code int `json:"code"`
		Data struct {
			AccessToken string `json:"access_token"`
		} `json:"data"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&res); err != nil {
		return "", err
	}
	if res.Code != 0 || res.Data.AccessToken == "" {
		return "", fmt.Errorf("login response code %d", res.Code)
	}
	return res.Data.AccessToken, nil
}

func fetchTicket(client *http.Client, target, token string) (string, error) {
	req, err := http.NewRequest(http.MethodPost, target+"/api/v1/auth/ticket", nil)
	if err != nil {
		return "", err
	}
	req.Header.Set("Authorization", "Bearer "+token)
	resp, err := client.Do(req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()

	var res struct {
		Code int `json:"code"`
		Data struct {
			Ticket string `json:"ticket"`
		} `json:"data"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&res); err != nil {
		return "", err
	}
	if res.Code != 0 || res.Data.Ticket == "" {
		return "", fmt.Errorf("ticket code %d", res.Code)
	}
	return res.Data.Ticket, nil
}

func toWsURL(target string) string {
	u, err := url.Parse(target)
	if err != nil {
		return "wss://106.52.170.56:8080"
	}
	if u.Scheme == "https" {
		u.Scheme = "wss"
	} else {
		u.Scheme = "ws"
	}
	return u.String()
}
