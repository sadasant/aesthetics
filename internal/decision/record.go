package decision

import (
	"context"
	"sync"
	"time"
)

// Exchange is one recorded request and its outcome.
type Exchange struct {
	Time     time.Time `json:"time"`
	Duration string    `json:"duration"`
	Request  Request   `json:"request"`
	Response *Response `json:"response,omitempty"`
	Error    string    `json:"error,omitempty"`
}

// Recorder wraps a provider and keeps every exchange, for reproducibility
// records. It never sees credentials.
type Recorder struct {
	Provider
	mu        sync.Mutex
	exchanges []Exchange
}

func (r *Recorder) Decide(ctx context.Context, req Request) (*Response, error) {
	start := time.Now()
	resp, err := r.Provider.Decide(ctx, req)
	ex := Exchange{Time: start.UTC(), Duration: time.Since(start).Round(time.Millisecond).String(), Request: req, Response: resp}
	if err != nil {
		ex.Error = err.Error()
	}
	r.mu.Lock()
	r.exchanges = append(r.exchanges, ex)
	r.mu.Unlock()
	return resp, err
}

// Exchanges returns the recorded exchanges in completion order.
func (r *Recorder) Exchanges() []Exchange {
	r.mu.Lock()
	defer r.mu.Unlock()
	return append([]Exchange(nil), r.exchanges...)
}
