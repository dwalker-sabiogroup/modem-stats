package utils

import (
	"crypto/md5"
	"crypto/tls"
	"fmt"
	"io"
	"math/rand/v2"
	"net/http"
	"os"
	"strconv"
	"strings"
	"time"

	"github.com/Jeffail/gabs/v2"
)

var insecureHTTPClient = &http.Client{
	Timeout: 30 * time.Second,
	Transport: &http.Transport{
		TLSClientConfig: &tls.Config{InsecureSkipVerify: true},
	},
}

// InsecureHTTPClient returns an HTTP client that skips TLS verification
func InsecureHTTPClient() *http.Client {
	return insecureHTTPClient
}

func SimpleHTTPFetch(url string) ([]byte, int64, error) {
	timeStart := time.Now().UnixMilli()
	resp, err := insecureHTTPClient.Get(url)
	if err != nil {
		return nil, 0, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != 200 {
		return nil, 0, fmt.Errorf("%d status code recieved", resp.StatusCode)
	}

	stats, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, 0, err
	}
	fetchTime := time.Now().UnixMilli() - timeStart
	return stats, fetchTime, nil
}

func RandomInt(min int, max int) int {
	return rand.IntN(max-min) + min
}

func StringToMD5(input string) string {
	return fmt.Sprintf("%x", md5.Sum([]byte(input)))
}

func GabsInt(input *gabs.Container, path string) int {
	output, _ := strconv.Atoi(input.Path(path).String())
	return output
}

func GabsFloat(input *gabs.Container, path string) float64 {
	output, _ := strconv.ParseFloat(input.Path(path).String(), 64)
	return output
}

func GabsString(input *gabs.Container, path string) string {
	output := input.Path(path).String()
	return strings.Trim(output, "\"")
}

func Getenv(key, fallback string) string {
	value := os.Getenv(key)
	if len(value) == 0 {
		return fallback
	}
	return value
}

func FetchStats(router DocsisModem) (ModemStats, error) {
	stats, err := router.ParseStats()
	return stats, err
}

func ResetStats(router DocsisModem) {
	router.ClearStats()
}

type HttpResult struct {
	Index int
	Res   http.Response
	Err   error
}

func BoundedParallelGet(urls []string, concurrencyLimit int) []HttpResult {
	semaphoreChan := make(chan struct{}, concurrencyLimit)
	resultsChan := make(chan *HttpResult, len(urls))

	for i, url := range urls {
		go func(i int, url string) {
			semaphoreChan <- struct{}{}
			res, err := insecureHTTPClient.Get(url)
			var result *HttpResult
			if res != nil {
				result = &HttpResult{i, *res, err}
			} else {
				result = &HttpResult{Index: i, Err: err}
			}
			resultsChan <- result
			<-semaphoreChan
		}(i, url)
	}

	results := make([]HttpResult, 0, len(urls))
	for range urls {
		result := <-resultsChan
		results = append(results, *result)
	}
	close(semaphoreChan)
	close(resultsChan)

	return results
}

func ExtractIntValue(valueWithUnit string) int {
	parts := strings.Split(valueWithUnit, " ")
	if len(parts) > 0 {
		intValue, err := strconv.Atoi(parts[0])
		if err == nil {
			return intValue
		}
	}
	return 0
}

func ExtractFloatValue(valueWithUnit string) float64 {
	parts := strings.Split(valueWithUnit, " ")
	if len(parts) > 0 {
		floatValue, err := strconv.ParseFloat(parts[0], 64)
		if err == nil {
			return floatValue
		}
	}
	return 0.0
}
