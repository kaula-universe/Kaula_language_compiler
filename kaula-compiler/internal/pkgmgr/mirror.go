package pkgmgr

import (
	"fmt"
	"io"
	"net/http"
	"sync"
	"time"
)

// MirrorTestResult 镜像测速结果
type MirrorTestResult struct {
	URL      string
	Latency  time.Duration // 延迟（RTT）
	Speed    float64       // 速度（bytes/sec）
	Success  bool
	Error    error
}

// TestMirror 测试单个镜像的延迟和速度
// 发送 Range 请求下载前 64KB 来测量
func TestMirror(url string, timeout time.Duration) MirrorTestResult {
	result := MirrorTestResult{URL: url}

	client := &http.Client{Timeout: timeout}

	start := time.Now()
	req, err := http.NewRequest("GET", url, nil)
	if err != nil {
		result.Error = err
		return result
	}
	// 只下载前 64KB 来测速
	req.Header.Set("Range", "bytes=0-65535")

	resp, err := client.Do(req)
	if err != nil {
		result.Error = err
		return result
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK && resp.StatusCode != http.StatusPartialContent {
		result.Error = fmt.Errorf("HTTP %d", resp.StatusCode)
		return result
	}

	// 读取 64KB 数据
	n, err := io.Copy(io.Discard, resp.Body)
	elapsed := time.Since(start)

	if err != nil && err != io.EOF {
		result.Error = err
		return result
	}

	result.Latency = elapsed
	result.Speed = float64(n) / elapsed.Seconds()
	result.Success = true
	return result
}

// SelectBestMirror 并发测试所有镜像，选择最快的
// 返回最佳镜像 URL 和详细测速结果
func SelectBestMirror(mirrors []string, timeout time.Duration) (string, []MirrorTestResult) {
	if len(mirrors) == 0 {
		return "", nil
	}
	if len(mirrors) == 1 {
		return mirrors[0], []MirrorTestResult{{URL: mirrors[0], Success: true}}
	}

	results := make([]MirrorTestResult, len(mirrors))
	var wg sync.WaitGroup

	fmt.Printf("  Testing %d mirrors...\n", len(mirrors))

	for i, mirror := range mirrors {
		wg.Add(1)
		go func(idx int, m string) {
			defer wg.Done()
			results[idx] = TestMirror(m, timeout)
			if results[idx].Success {
				fmt.Printf("  [OK] %s - %dms, %.1f KB/s\n",
					m, results[idx].Latency.Milliseconds(),
					results[idx].Speed/1024)
			} else {
				fmt.Printf("  [FAIL] %s - %v\n", m, results[idx].Error)
			}
		}(i, mirror)
	}

	wg.Wait()

	// 选择速度最快的镜像
	bestIdx := -1
	var bestSpeed float64
	for i, r := range results {
		if r.Success {
			if bestIdx == -1 || r.Speed > bestSpeed {
				bestIdx = i
				bestSpeed = r.Speed
			}
		}
	}

	if bestIdx == -1 {
		// 所有镜像都失败，返回第一个
		return mirrors[0], results
	}

	fmt.Printf("  Best mirror: %s (%.1f KB/s)\n", mirrors[bestIdx], bestSpeed/1024)
	return mirrors[bestIdx], results
}
