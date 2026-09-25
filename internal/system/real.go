package system

import (
	"bufio"
	"fmt"
	"io"
	"net/http"
	"os"
	"strings"
	"time"
)

// Terminal is the Prompter that reads the real stdin.
type Terminal struct {
	// lines is kept across questions, since a fresh reader would drop what the last one buffered.
	lines *bufio.Reader
}

// Interactive says whether stdin is a terminal, which is where there is someone to ask.
func (*Terminal) Interactive() bool {
	return IsTerminal(os.Stdin)
}

// Ask puts a question on stdout and reads the line typed back, or reports the input ending.
func (t *Terminal) Ask(question string) (string, bool) {
	if _, err := io.WriteString(os.Stdout, question); err != nil {
		return "", false
	}
	if t.lines == nil {
		t.lines = bufio.NewReader(os.Stdin)
	}
	line, err := t.lines.ReadString('\n')
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
		return nil, fmt.Errorf("%s answered %s", url, response.Status)
	}
	return io.ReadAll(response.Body)
}
