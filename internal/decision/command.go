package decision

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"os/exec"
	"strings"
	"time"
)

// commandProvider runs an external program for each request. The program
// receives a System One request as JSON on stdin and must print a System One
// response as JSON on stdout. No shell is involved, so it works the same on
// every operating system.
type commandProvider struct {
	info    Info
	argv    []string
	timeout time.Duration
}

func (p *commandProvider) Info() Info { return p.info }

func (p *commandProvider) Decide(ctx context.Context, req Request) (*Response, error) {
	input, err := json.Marshal(systemOneBody(p.info.Model, req))
	if err != nil {
		return nil, err
	}
	ctx, cancel := context.WithTimeout(ctx, p.timeout)
	defer cancel()
	cmd := exec.CommandContext(ctx, p.argv[0], p.argv[1:]...)
	cmd.Stdin = bytes.NewReader(input)
	var stdout, stderr bytes.Buffer
	cmd.Stdout, cmd.Stderr = &stdout, &stderr
	if err := cmd.Run(); err != nil {
		msg := strings.TrimSpace(stderr.String())
		if len(msg) > 300 {
			msg = msg[:300] + "…"
		}
		return nil, fmt.Errorf("provider command %q failed: %v %s", p.argv[0], err, msg)
	}
	return parseSystemOne(stdout.Bytes())
}
