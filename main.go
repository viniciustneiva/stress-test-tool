package main

import (
	"bytes"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"net/http"
	"os"
	"sync"
	"sync/atomic"
	"time"
)

type Config struct {
	URL         string
	Method      string
	HeaderFile  string
	BodyFile    string
	Requests    int
	Concurrency int
}

type Results struct {
	TotalRequests   int64
	SuccessRequests int64
	FailedRequests  int64
	TotalTime       time.Duration
	AverageDuration time.Duration
	MinDuration     time.Duration
	MaxDuration     time.Duration
}

func loadJSONFile(filePath string) (map[string]any, error) {
	if filePath == "" {
		return map[string]any{}, nil
	}

	data, err := os.ReadFile(filePath)
	if err != nil {
		return nil, fmt.Errorf("erro ao ler arquivo %s: %v", filePath, err)
	}

	var result map[string]any
	err = json.Unmarshal(data, &result)
	if err != nil {
		return nil, fmt.Errorf("erro ao fazer parse do JSON em %s: %v", filePath, err)
	}

	return result, nil
}

func makeRequest(config Config, headers map[string]any, body map[string]any) (time.Duration, error) {
	client := &http.Client{
		Timeout: 30 * time.Second,
	}

	var bodyReader io.Reader
	if len(body) > 0 {
		bodyBytes, _ := json.Marshal(body)
		bodyReader = io.NopCloser(io.Reader(bytes.NewReader(bodyBytes)))
	}

	req, err := http.NewRequest(config.Method, config.URL, bodyReader)
	if err != nil {
		return 0, err
	}

	// Adicionar headers
	for key, value := range headers {
		req.Header.Set(key, fmt.Sprintf("%v", value))
	}

	if len(body) > 0 {
		req.Header.Set("Content-Type", "application/json")
	}

	start := time.Now()
	resp, err := client.Do(req)
	duration := time.Since(start)

	if err != nil {
		return duration, err
	}
	defer resp.Body.Close()

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return duration, fmt.Errorf("status code: %d", resp.StatusCode)
	}

	return duration, nil
}

func runStressTest(config Config, headers map[string]any, body map[string]any) Results {
	results := Results{}
	var (
		successCount int64
		failedCount  int64
		totalTime    int64
		minDuration  = time.Duration(1<<63 - 1)
		maxDuration  time.Duration
		mu           sync.Mutex
	)

	semaphore := make(chan struct{}, config.Concurrency)
	var wg sync.WaitGroup

	fmt.Printf("Iniciando stress test...\n")
	fmt.Printf("URL: %s\n", config.URL)
	fmt.Printf("Método: %s\n", config.Method)
	fmt.Printf("Requisições: %d\n", config.Requests)
	fmt.Printf("Concorrência: %d\n\n", config.Concurrency)

	startTime := time.Now()

	for i := 0; i < config.Requests; i++ {
		wg.Go(func() {
			semaphore <- struct{}{}
			defer func() { <-semaphore }()

			duration, err := makeRequest(config, headers, body)

			mu.Lock()
			atomic.AddInt64(&totalTime, duration.Nanoseconds())
			if duration < minDuration {
				minDuration = duration
			}
			if duration > maxDuration {
				maxDuration = duration
			}
			mu.Unlock()

			if err != nil {
				atomic.AddInt64(&failedCount, 1)
			} else {
				atomic.AddInt64(&successCount, 1)
			}
		})
	}

	wg.Wait()
	results.TotalTime = time.Since(startTime)
	results.TotalRequests = int64(config.Requests)
	results.SuccessRequests = atomic.LoadInt64(&successCount)
	results.FailedRequests = atomic.LoadInt64(&failedCount)
	results.AverageDuration = time.Duration(totalTime / int64(config.Requests))
	results.MinDuration = minDuration
	results.MaxDuration = maxDuration

	return results
}

func printResults(results Results) {
	fmt.Println("\n=== Resultados do Stress Test ===")
	fmt.Printf("Total de requisições: %d\n", results.TotalRequests)
	fmt.Printf("Requisições bem-sucedidas: %d\n", results.SuccessRequests)
	fmt.Printf("Requisições falhadas: %d\n", results.FailedRequests)
	fmt.Printf("Tempo total: %v\n", results.TotalTime)
	fmt.Printf("Tempo médio por requisição: %v\n", results.AverageDuration)
	fmt.Printf("Tempo mínimo: %v\n", results.MinDuration)
	fmt.Printf("Tempo máximo: %v\n", results.MaxDuration)
	fmt.Printf("Taxa de sucesso: %.2f%%\n", float64(results.SuccessRequests)/float64(results.TotalRequests)*100)
}

func main() {
	config := Config{}
	config.URL = "http://localhost:8080/ping"
	config.Method = "GET"


	flag.StringVar(&config.URL, "url", "", "URL para fazer stress test (obrigatório)")
	flag.StringVar(&config.Method, "method", "GET", "Método HTTP (GET, POST, PUT, DELETE, etc)")
	flag.StringVar(&config.HeaderFile, "headers", "", "Arquivo JSON com headers")
	flag.StringVar(&config.BodyFile, "body", "", "Arquivo JSON com body da requisição")
	flag.IntVar(&config.Requests, "requests", 100, "Número de requisições")
	flag.IntVar(&config.Concurrency, "concurrency", 10, "Número de requisições concorrentes")

	flag.Parse()

	if config.URL == "" {
		fmt.Println("Erro: -url é obrigatório")
		flag.PrintDefaults()
		os.Exit(1)
	}

	headers, err := loadJSONFile(config.HeaderFile)
	if err != nil {
		fmt.Printf("Erro ao carregar headers: %v\n", err)
		os.Exit(1)
	}

	body, err := loadJSONFile(config.BodyFile)
	if err != nil {
		fmt.Printf("Erro ao carregar body: %v\n", err)
		os.Exit(1)
	}

	results := runStressTest(config, headers, body)
	printResults(results)
}
