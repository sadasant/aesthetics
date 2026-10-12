// Package decision talks to decision models: models that answer typed
// questions about a shared state with calibrated probabilities instead of
// generating text.
//
// Aesthetics asks two question types:
//   - noul: a yes/no question answered with the probability of "yes"
//     (called a predicate in the OpenAI Decisions API);
//   - choice: a selection among named options with a probability for each.
package decision

import (
	"bytes"
	"cmp"
	"context"
	"encoding/json"
	"fmt"
	"os"
	"strings"
	"time"

	"go.yaml.in/yaml/v3"
)

// QuestionType is the type of a decision question.
type QuestionType string

const (
	Noul   QuestionType = "noul"
	Choice QuestionType = "choice"
)

// Option is one possible answer to a choice question.
type Option struct {
	Value       string `json:"value"`
	Description string `json:"description"`
}

// Question is a typed question about the request state.
type Question struct {
	ID           string       `json:"id"`
	Type         QuestionType `json:"type"`
	Instructions string       `json:"instructions"`
	True         string       `json:"true,omitempty"`  // noul: what "yes" means
	False        string       `json:"false,omitempty"` // noul: what "no" means
	Options      []Option     `json:"options,omitempty"`
}

// Answer is a provider's typed answer to one question.
type Answer struct {
	Type          QuestionType       `json:"type"`
	Probability   float64            `json:"probability,omitempty"` // noul: P(yes)
	Choice        string             `json:"choice,omitempty"`
	Probabilities map[string]float64 `json:"probabilities,omitempty"`
	Confidence    float64            `json:"confidence,omitempty"`
	Refused       bool               `json:"refused,omitempty"`
}

// Field is one entry of an ordered state object.
type Field struct {
	Key   string
	Value any
}

// State is the shared evidence every question is evaluated against. It
// serializes as a JSON object that preserves field order.
type State []Field

func (s State) MarshalJSON() ([]byte, error) {
	var b bytes.Buffer
	b.WriteByte('{')
	for i, f := range s {
		if i > 0 {
			b.WriteByte(',')
		}
		k, _ := json.Marshal(f.Key)
		v, err := json.Marshal(f.Value)
		if err != nil {
			return nil, err
		}
		b.Write(k)
		b.WriteByte(':')
		b.Write(v)
	}
	b.WriteByte('}')
	return b.Bytes(), nil
}

// Text renders the state as indented JSON, for providers that take text.
func (s State) Text() string {
	b, _ := json.MarshalIndent(s, "", "  ")
	return string(b)
}

// Request is one decision request.
type Request struct {
	State     State      `json:"state"`
	Questions []Question `json:"questions"`
}

// Usage reports token usage when the provider returns it.
type Usage struct {
	InputTokens  int `json:"input_tokens"`
	OutputTokens int `json:"output_tokens"`
}

// Response holds the answers keyed by question ID.
type Response struct {
	Model   string            `json:"model,omitempty"`
	Answers map[string]Answer `json:"answers"`
	Usage   Usage             `json:"usage"`
}

// Info describes a configured provider. It never contains credentials.
type Info struct {
	Type          string `json:"type"`
	Model         string `json:"model"`
	Endpoint      string `json:"endpoint,omitempty"`
	MaxStateChars int    `json:"max_state_chars"`
	MaxQuestions  int    `json:"max_questions"`
}

// Label names the provider and model for humans.
func (i Info) Label() string {
	if i.Model == "" {
		return i.Type
	}
	return i.Type + " (" + i.Model + ")"
}

// Provider answers decision requests.
type Provider interface {
	Decide(ctx context.Context, req Request) (*Response, error)
	Info() Info
}

// Duration is a time.Duration written as "90s" or "2m" in YAML.
type Duration time.Duration

func (d *Duration) UnmarshalYAML(n *yaml.Node) error {
	var s string
	if err := n.Decode(&s); err != nil {
		return err
	}
	v, err := time.ParseDuration(s)
	if err != nil {
		return fmt.Errorf("invalid duration %q: %w", s, err)
	}
	*d = Duration(v)
	return nil
}

// Config selects and configures a provider. Credentials are always read
// from environment variables, never from the configuration file.
type Config struct {
	Type          string            `yaml:"type"`
	Model         string            `yaml:"model,omitempty"`
	BaseURL       string            `yaml:"base_url,omitempty"`
	APIKeyEnv     string            `yaml:"api_key_env,omitempty"`
	AccountID     string            `yaml:"account_id,omitempty"`
	AccountIDEnv  string            `yaml:"account_id_env,omitempty"`
	Command       []string          `yaml:"command,omitempty"`
	Headers       map[string]string `yaml:"headers,omitempty"`
	Timeout       Duration          `yaml:"timeout,omitempty"`
	MaxStateChars int               `yaml:"max_state_chars,omitempty"`
	MaxQuestions  int               `yaml:"max_questions,omitempty"`
}

// UnmarshalYAML accepts "provider: typesafe" as shorthand for the type.
func (c *Config) UnmarshalYAML(n *yaml.Node) error {
	if n.Kind == yaml.ScalarNode {
		return n.Decode(&c.Type)
	}
	type plain Config
	return n.Decode((*plain)(c))
}

// Types lists the supported provider types in integration priority order.
var Types = []string{"openai", "typesafe", "cloudflare", "laya", "systemone", "chat", "command"}

type defaults struct {
	baseURL, path, keyEnv, model string
	keyRequired                  bool
	maxState, maxQuestions       int
}

// Defaults verified against provider documentation in October 2026.
var typeDefaults = map[string]defaults{
	// OpenAI Decisions API: POST /v1/decisions; gpt-6-luna is the only model.
	"openai": {baseURL: "https://api.openai.com/v1", path: "/decisions", keyEnv: "OPENAI_API_KEY", model: "gpt-6-luna", keyRequired: true, maxState: 400000, maxQuestions: 16},
	// TypeSafe System One API: POST /v1/systemone; 32k-token context.
	"typesafe": {baseURL: "https://api.typesafe.ai/v1", path: "/systemone", keyEnv: "TYPESAFE_API_KEY", model: "jev-latest", keyRequired: true, maxState: 90000, maxQuestions: 16},
	// Cloudflare Workers AI: POST /accounts/{id}/ai/run/@cf/cloudflare/{clef|clef-flash}.
	// Clef reads only the first ~2k tokens of text state; long state is truncated.
	"cloudflare": {baseURL: "https://api.cloudflare.com/client/v4", keyEnv: "CLOUDFLARE_API_TOKEN", model: "clef", keyRequired: true, maxState: 8000, maxQuestions: 16},
	// laya-serve exposes the System One API on port 8000; 8k-token context.
	"laya": {baseURL: "http://localhost:8000/v1", path: "/systemone", keyEnv: "LAYA_API_KEY", model: "laya", maxState: 24000, maxQuestions: 8},
	// Any other System One-compatible endpoint, such as OpenRouter.
	"systemone": {path: "/systemone", maxState: 60000, maxQuestions: 8},
	// Any OpenAI-compatible chat completions endpoint, emulating decisions.
	"chat": {baseURL: "https://api.openai.com/v1", path: "/chat/completions", keyEnv: "OPENAI_API_KEY", maxState: 100000, maxQuestions: 16},
	// An external program speaking the System One JSON protocol on stdin/stdout.
	"command": {maxState: 100000, maxQuestions: 16},
}

// Describe returns the provider's identity and limits without touching
// credentials, for planning and explanation.
func Describe(cfg Config) (Info, error) {
	t := strings.ToLower(strings.TrimSpace(cfg.Type))
	d, ok := typeDefaults[t]
	if !ok {
		if t == "" {
			return Info{}, fmt.Errorf("no model provider configured: set provider.type in the configuration, pass --provider, or set AESTHETICS_PROVIDER (one of %s)", strings.Join(Types, ", "))
		}
		return Info{}, fmt.Errorf("unknown provider type %q (want one of %s)", cfg.Type, strings.Join(Types, ", "))
	}
	info := Info{Type: t, Model: cfg.Model, MaxStateChars: d.maxState, MaxQuestions: d.maxQuestions}
	if info.Model == "" {
		info.Model = d.model
	}
	if cfg.MaxStateChars > 0 {
		info.MaxStateChars = cfg.MaxStateChars
	}
	if cfg.MaxQuestions > 0 {
		info.MaxQuestions = cfg.MaxQuestions
	}
	base := strings.TrimRight(cfg.BaseURL, "/")
	if base == "" {
		base = d.baseURL
	}
	switch t {
	case "command":
		info.Endpoint = strings.Join(cfg.Command, " ")
	case "cloudflare":
		info.Endpoint, _, _ = cloudflareEndpoint(cfg, base, info.Model)
	default:
		info.Endpoint = base
		if base != "" && !strings.HasSuffix(base, d.path) {
			info.Endpoint = base + d.path
		}
	}
	return info, nil
}

// New builds the provider described by cfg.
func New(cfg Config) (Provider, error) {
	info, err := Describe(cfg)
	if err != nil {
		return nil, err
	}
	t, d := info.Type, typeDefaults[info.Type]
	timeout := time.Duration(cfg.Timeout)
	if timeout <= 0 {
		timeout = 2 * time.Minute
	}
	if t == "command" {
		if len(cfg.Command) == 0 {
			return nil, fmt.Errorf("provider type command requires provider.command, for example [\"python3\", \"provider.py\"]")
		}
		return &commandProvider{info: info, argv: cfg.Command, timeout: timeout}, nil
	}
	if info.Model == "" && t != "systemone" {
		return nil, fmt.Errorf("provider %s requires a model (provider.model or --model)", t)
	}
	if info.Endpoint == "" {
		return nil, fmt.Errorf("provider %s requires provider.base_url", t)
	}
	keyEnv := cfg.APIKeyEnv
	if keyEnv == "" {
		keyEnv = d.keyEnv
	}
	var key string
	if keyEnv != "" {
		key = os.Getenv(keyEnv)
		if key == "" && (d.keyRequired || cfg.APIKeyEnv != "") {
			return nil, fmt.Errorf("provider %s needs an API key in the environment variable %s", t, keyEnv)
		}
	}
	c := &httpClient{key: key, headers: cfg.Headers, timeout: timeout}
	switch t {
	case "openai":
		return &openAIProvider{info: info, http: c}, nil
	case "chat":
		return &chatProvider{info: info, http: c}, nil
	case "cloudflare":
		endpoint, selector, err := cloudflareEndpoint(cfg, strings.TrimRight(cmp.Or(cfg.BaseURL, d.baseURL), "/"), info.Model)
		if err != nil {
			return nil, err
		}
		info.Endpoint = endpoint
		return &systemOneProvider{info: info, http: c, model: selector}, nil
	default: // typesafe, laya, systemone
		return &systemOneProvider{info: info, http: c, model: info.Model}, nil
	}
}

func cloudflareEndpoint(cfg Config, base, model string) (endpoint, selector string, err error) {
	if cfg.BaseURL != "" && strings.Contains(cfg.BaseURL, "/ai/run") {
		base = strings.TrimRight(cfg.BaseURL, "/")
	} else {
		account := cfg.AccountID
		env := cfg.AccountIDEnv
		if env == "" {
			env = "CLOUDFLARE_ACCOUNT_ID"
		}
		if account == "" {
			account = os.Getenv(env)
		}
		if account == "" {
			return "", "", fmt.Errorf("provider cloudflare needs an account ID in provider.account_id or the environment variable %s", env)
		}
		base = base + "/accounts/" + account + "/ai/run"
	}
	path := model
	if !strings.HasPrefix(model, "@") {
		path = "@cf/cloudflare/" + model
	}
	return base + "/" + path, path[strings.LastIndex(path, "/")+1:], nil
}
