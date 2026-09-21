package system

import (
	"bufio"
	"io"
	"net/http"
	"os"
	"strings"
	"time"
)

// Terminal is the Prompter that reads the real stdin.
type Terminal struct{}

// Interactive says whether stdin is a terminal, which is where there is someone to ask.
func (Terminal) Interactive() bool {
	return IsTerminal(os.Stdin)
}

// Ask puts a question on stdout and reads the line typed back, or reports the input ending.
func (Terminal) Ask(question string) (string, bool) {
	if _, err := io.WriteString(os.Stdout, question); err != nil {
		return "", false
	}
	line, err := bufio.NewReader(os.Stdin).ReadString('\n')
	if err != nil && line == "" {
		return "", false
	}
	return strings.TrimRight(line, "\r\n"), true
}

// Web is the Downloader that really makes the request.
type Web struct {
	Timeout time.Duration
}

// Get reads a URL, refusing an answer that is not a plain success.
func (w Web) Get(url string) ([]byte, error) {
	client := http.Client{Timeout: w.Timeout}
	response, err := client.Get(url)
	if err != nil {
		return nil, err
	}
	defer func() { _ = response.Body.Close() }()
	if response.StatusCode != http.StatusOK {
		return nil, &httpError{url: url, status: response.Status}
	}
	return io.ReadAll(response.Body)
}

// httpError is an answer that came back but was not the one asked for.
type httpError struct {
	url    string
	status string
}

// Error names the URL and what it answered with.
func (e *httpError) Error() string {
	return e.url + " answered " + e.status
}
