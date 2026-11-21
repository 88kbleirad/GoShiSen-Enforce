// Package config defines the daemon configuration model, its YAML file
// encoding, and the loading precedence: defaults < file < environment <
// flags. It performs no system modification; the only system access is
// read-only validation (interface lookup, policy file readability).
package config

import (
	"errors"
	"fmt"
	"net"
	"net/netip"
	"os"
	"path/filepath"
	"strconv"
	"strings"

	"gopkg.in/yaml.v3"
)

// EnvPrefix prefixes every environment variable this package reads.
const EnvPrefix = "GOSHISEN_"

// Hook scope values (mirrored from internal/hook; kept here to avoid an
// import cycle since hook imports config).
const (
	HookScopeForwardAll = "forward-all"
	HookScopeOpenWrt    = "openwrt"
)

// Defaults for configurable values.
const (
	DefaultLogLevel = "info"
	DefaultLogJSON  = false

	DefaultAPIHost       = "127.0.0.1"
	DefaultAPIPort       = 8080
	DefaultMetricsPort   = 9090
	DefaultPolicyDir     = "policy_store"
	DefaultQueueMaxlen   = 1024
	DefaultOffloadPolicy = "warn"
	// OffloadPolicyWarn, OffloadPolicyRefuse, OffloadPolicyReadiness,
	// OffloadPolicyEnforce are the accepted values for File.OffloadPolicy.
	OffloadPolicyWarn      = "warn"
	OffloadPolicyRefuse    = "refuse"
	OffloadPolicyReadiness = "readiness"
	OffloadPolicyEnforce   = "enforce"
)

// File is the YAML configuration file schema. Unknown keys are rejected.
type File struct {
	// Interface is the network interface the firewall attaches to.
	// Empty means "auto": the first up, non-loopback interface.
	Interface string `yaml:"interface"`

	// PolicyFile is the path to the YAML policy ruleset. Required.
	PolicyFile string `yaml:"policy_file"`

	// LogLevel is one of debug, info, warn, error. Default "info".
	LogLevel string `yaml:"log_level"`

	// LogJSON switches log output from text to JSON. Default false.
	LogJSON bool `yaml:"log_json"`

	// APIHost is the API bind host. Default 127.0.0.1 (localhost only).
	// A non-loopback value is rejected unless AllowNonLoopbackAPIHost is
	// set.
	APIHost string `yaml:"api_host"`

	// AllowNonLoopbackAPIHost opts into binding the API to a
	// non-loopback address. Default false (loopback only): a non-loopback
	// api_host exposes the administrative API to the network, so it must
	// be requested explicitly.
	AllowNonLoopbackAPIHost bool `yaml:"allow_non_loopback_api_host"`

	// APIPort is the API TCP port; default 8080 (loopback) when no
	// layer sets it. An explicit 0 — file, GOSHISEN_API_PORT, or
	// --api-port — disables the API listener (see loadFile). Releases up
	// to and including v0.2.6 ignored a file/flag 0 and fell back to
	// 8080; the presence tracking below is the fix (unreleased in the
	// v0.2.6 tag, on main afterwards).
	APIPort int `yaml:"api_port"`

	// APIToken is the bearer token required for administrative
	// operations. It is intentionally NOT accepted via CLI flags (argv
	// is visible to other users); set it via config file or
	// GOSHISEN_API_TOKEN. Empty disables administrative mutations.
	APIToken string `yaml:"api_token"`

	// RequireTokenForReads (opt-in, default false) additionally requires
	// the bearer token for the policy read endpoints (GET /v1/policy,
	// GET /v1/policy/versions). /healthz and /readyz stay open. This is
	// a hardening opt-in: with a token configured but this unset, reads
	// remain unauthenticated (historical behavior).
	RequireTokenForReads bool `yaml:"require_token_for_reads"`

	// MetricsTokenRequired (opt-in, default false) requires the bearer
	// token for GET /metrics. Without it, metrics stay open (Prometheus
	// scraping without auth).
	MetricsTokenRequired bool `yaml:"metrics_token_required"`

	// MetricsPort is the Prometheus metrics TCP port; default 9090
	// (loopback) when no layer sets it. An explicit 0 — file,
	// GOSHISEN_METRICS_PORT, or --metrics-port — disables the metrics
	// listener (see loadFile). Same ≤ v0.2.6 caveat as APIPort.
	MetricsPort int `yaml:"metrics_port"`

	// apiPortSet/metricsPortSet record whether the config file explicitly
	// set api_port/metrics_port. A resolved 0 disables the listener, so
	// presence must be tracked (int zero cannot distinguish "0" from
	// "absent"); set by UnmarshalYAML.
	apiPortSet     bool
	metricsPortSet bool

	// PolicyDir is the durable policy store directory (versioned policy
	// files + active pointer). Default "policy_store".
	PolicyDir string `yaml:"policy_dir"`

	// QueueMaxlen is the kernel NFQUEUE queue length in packets. A
	// smaller queue spills earlier and is useful for overflow
	// observability and testing. Default 1024.
	QueueMaxlen int `yaml:"queue_maxlen"`

	// QueueNum is the NFQUEUE queue number to bind. Default 0.
	QueueNum int `yaml:"queue_num"`

	// HookScope selects the nftables hook placement. Empty (default) is
	// "forward-all" (queue all forwarded tcp/udp). The value
	// "openwrt" places the hook after firewall4's forward chain
	// (priority 1, policy accept) and creates no NAT chains.
	HookScope string `yaml:"hook_scope"`

	// OffloadPolicy controls what the daemon does when nftables flow
	// offloading is detected: established flows skip NFQUEUE, so
	// enforcement does not apply to them.
	//   warn      (default) log a WARN and expose the health gauge.
	//   refuse    exit at startup with an error (never install the hook).
	//   readiness keep enforcing but never become ready (readiness stays
	//             false while offloading is active).
	//   enforce   actively disable the offload (verified); if the
	//             disable does not take effect, or detection is unknown,
	//             fail closed at startup (never install the hook).
	OffloadPolicy string `yaml:"offload_policy"`

	// AllowBypass is the explicit diagnostic/development escape hatch
	// for offload_policy=enforce: when true it downgrades enforce to
	// warn semantics (loud warning, cap stays open). Default false.
	// Never a silent fallback: without it, enforce stays fail-closed.
	AllowBypass bool `yaml:"allow_bypass"`

	// Nat configures kernel-side NAT (nftables dstnat/srcnat chains).
	Nat NatConfig `yaml:"nat"`
}

// NatConfig is the NAT configuration (kernelside, transactionally
// applied with the filter ruleset).
type NatConfig struct {
	// Enabled is the master switch. Default false.
	Enabled bool `yaml:"enabled"`

	// Masquerade configures outbound source NAT.
	Masquerade MasqueradeConfig `yaml:"masquerade"`

	// Dnat is an ordered list of DNAT mappings (evaluated in order).
	Dnat []DnatRule `yaml:"dnat"`
}

// MasqueradeConfig configures MASQUERADE on an egress interface.
type MasqueradeConfig struct {
	Enabled bool `yaml:"enabled"`
	// OutIface is the egress interface whose traffic is masqueraded.
	OutIface string `yaml:"out_iface"`
}

// DnatRule maps a public (pre-NAT) destination to an internal target.
type DnatRule struct {
	Name       string `yaml:"name"`
	Protocol   string `yaml:"protocol"` // tcp | udp
	PublicIP   string `yaml:"pub_ip"`
	PublicPort int    `yaml:"pub_port"`
	TargetIP   string `yaml:"target_ip"`
	TargetPort int    `yaml:"target_port"`
}

// Overrides carries CLI values. A nil field means "flag not provided", so
// an explicit empty string still overrides lower layers. APIToken is
// deliberately not here: secrets are never accepted from argv.
type Overrides struct {
	Interface   *string
	PolicyFile  *string
	LogLevel    *string
	LogJSON     *bool
	APIHost     *string
	APIPort     *int
	MetricsPort *int
	PolicyDir   *string
	QueueMaxlen *int

	// AllowNonLoopbackAPIHost opts into a non-loopback API bind.
	AllowNonLoopbackAPIHost *bool

	// RequireTokenForReads / MetricsTokenRequired opt into requiring the
	// bearer token for the read endpoints / the metrics endpoint.
	RequireTokenForReads *bool
	MetricsTokenRequired *bool
}

// Options is the resolved, validated runtime configuration.
type Options struct {
	File
	// ConfigFile is the path of the --config file, empty if none was used.
	ConfigFile string
	// Sources records, per field, which layer supplied the value:
	// "default", "file", "env", or "flag".
	Sources map[string]string
}

// UnmarshalYAML decodes the file schema and records which port keys were
// explicitly present, so the load layer can honor an explicit 0
// (disable). Unknown keys, and the known key spelled in a non-mapping
// form, are rejected.
func (f *File) UnmarshalYAML(node *yaml.Node) error {
	type rawFile File
	if node.Kind != yaml.MappingNode {
		return fmt.Errorf("config file: expected a mapping of settings, got %s", node.ShortTag())
	}
	var raw rawFile
	if err := node.Decode(&raw); err != nil {
		return err
	}
	*f = File(raw)
	for i := 0; i+1 < len(node.Content); i += 2 {
		key, val := node.Content[i], node.Content[i+1]
		if key.Kind != yaml.ScalarNode {
			return fmt.Errorf("config file: setting name must be a scalar, got %s", key.ShortTag())
		}
		switch key.Value {
		case "api_port":
			f.apiPortSet = true
			if val.Kind != yaml.ScalarNode {
				return fmt.Errorf("config file: api_port must be an integer, got %s", val.ShortTag())
			}
		case "metrics_port":
			f.metricsPortSet = true
			if val.Kind != yaml.ScalarNode {
				return fmt.Errorf("config file: metrics_port must be an integer, got %s", val.ShortTag())
			}
		}
	}
	return nil
}

// Load merges the four configuration layers. configFile may be empty to
// skip the file layer. env reads environment variables (normally
// os.LookupEnv). It returns an error describing every invalid value found.
func Load(configFile string, env func(string) (string, bool), ov Overrides) (*Options, error) {
	o := &Options{
		File: File{
			LogLevel:      DefaultLogLevel,
			LogJSON:       DefaultLogJSON,
			APIHost:       DefaultAPIHost,
			APIPort:       DefaultAPIPort,
			MetricsPort:   DefaultMetricsPort,
			PolicyDir:     DefaultPolicyDir,
			QueueMaxlen:   DefaultQueueMaxlen,
			OffloadPolicy: DefaultOffloadPolicy,
		},
		ConfigFile: configFile,
		Sources: map[string]string{
			"interface":                   "default",
			"policy_file":                 "default",
			"log_level":                   "default",
			"log_json":                    "default",
			"api_host":                    "default",
			"allow_non_loopback_api_host": "default",
			"api_port":                    "default",
			"api_token":                   "default",
			"require_token_for_reads":     "default",
			"metrics_token_required":      "default",
			"metrics_port":                "default",
			"policy_dir":                  "default",
			"queue_maxlen":                "default",
			"queue_num":                   "default",
			"hook_scope":                  "default",
			"offload_policy":              "default",
		},
	}

	if configFile != "" {
		if err := o.loadFile(configFile); err != nil {
			return nil, err
		}
	}
	if envErr := o.loadEnv(env); envErr != nil {
		return nil, envErr
	}
	o.loadOverrides(ov)

	if err := o.validate(); err != nil {
		return nil, err
	}
	return o, nil
}

func (o *Options) loadFile(path string) error {
	data, err := ReadFileBounded(path)
	if err != nil {
		return fmt.Errorf("config file %s: %w", path, err)
	}
	dec := yaml.NewDecoder(strings.NewReader(string(data)))
	dec.KnownFields(true)
	var f File
	if err := dec.Decode(&f); err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return fmt.Errorf("config file %s: %w", path, err)
		}
		return fmt.Errorf("config file %s: %w", path, err)
	}
	if f.Interface != "" {
		o.Interface = f.Interface
		o.Sources["interface"] = "file"
	}
	if f.PolicyFile != "" {
		o.PolicyFile = f.PolicyFile
		o.Sources["policy_file"] = "file"
	}
	if f.LogLevel != "" {
		o.LogLevel = f.LogLevel
		o.Sources["log_level"] = "file"
	}
	// LogJSON is read directly: a file cannot distinguish false from unset,
	// so the file layer may only turn JSON logging on. Disabling a true
	// value is the job of flags/env, which do track set-ness.
	if f.LogJSON {
		o.LogJSON = true
		o.Sources["log_json"] = "file"
	}
	if f.APIHost != "" {
		o.APIHost = f.APIHost
		o.Sources["api_host"] = "file"
	}
	if f.apiPortSet {
		o.APIPort = f.APIPort
		o.Sources["api_port"] = "file"
	}
	if f.APIToken != "" {
		o.APIToken = f.APIToken
		o.Sources["api_token"] = "file"
	}
	if f.metricsPortSet {
		o.MetricsPort = f.MetricsPort
		o.Sources["metrics_port"] = "file"
	}
	if f.PolicyDir != "" {
		o.PolicyDir = f.PolicyDir
		o.Sources["policy_dir"] = "file"
	}
	if f.QueueMaxlen != 0 {
		o.QueueMaxlen = f.QueueMaxlen
		o.Sources["queue_maxlen"] = "file"
	}
	if f.HookScope != "" {
		o.HookScope = f.HookScope
		o.Sources["hook_scope"] = "file"
	}
	if f.OffloadPolicy != "" {
		o.OffloadPolicy = f.OffloadPolicy
		o.Sources["offload_policy"] = "file"
	}
	if f.AllowBypass {
		o.AllowBypass = true
		o.Sources["allow_bypass"] = "file"
	}
	if f.AllowNonLoopbackAPIHost {
		o.AllowNonLoopbackAPIHost = true
		o.Sources["allow_non_loopback_api_host"] = "file"
	}
	if f.RequireTokenForReads {
		o.RequireTokenForReads = true
		o.Sources["require_token_for_reads"] = "file"
	}
	if f.MetricsTokenRequired {
		o.MetricsTokenRequired = true
		o.Sources["metrics_token_required"] = "file"
	}
	if f.QueueNum != 0 {
		o.QueueNum = f.QueueNum
		o.Sources["queue_num"] = "file"
	}
	o.Nat = f.Nat
	return nil
}

func (o *Options) loadEnv(env func(string) (string, bool)) error {
	var errs []error
	get := func(name string) (string, bool) {
		v, ok := env(EnvPrefix + name)
		if !ok || v == "" {
			return "", false
		}
		return v, true
	}
	if v, ok := get("INTERFACE"); ok {
		o.Interface = v
		o.Sources["interface"] = "env"
	}
	if v, ok := get("POLICY_FILE"); ok {
		o.PolicyFile = v
		o.Sources["policy_file"] = "env"
	}
	if v, ok := get("LOG_LEVEL"); ok {
		o.LogLevel = v
		o.Sources["log_level"] = "env"
	}
	if v, ok := get("LOG_JSON"); ok {
		b, err := parseBool(v)
		if err != nil {
			errs = append(errs, fmt.Errorf("%sLOG_JSON: %w", EnvPrefix, err))
		} else {
			o.LogJSON = b
			o.Sources["log_json"] = "env"
		}
	}
	if v, ok := get("API_HOST"); ok {
		o.APIHost = v
		o.Sources["api_host"] = "env"
	}
	if v, ok := get("ALLOW_NON_LOOPBACK_API_HOST"); ok {
		b, err := parseBool(v)
		if err != nil {
			errs = append(errs, fmt.Errorf("%sALLOW_NON_LOOPBACK_API_HOST: %w", EnvPrefix, err))
		} else {
			o.AllowNonLoopbackAPIHost = b
			o.Sources["allow_non_loopback_api_host"] = "env"
		}
	}
	if v, ok := get("REQUIRE_TOKEN_FOR_READS"); ok {
		b, err := parseBool(v)
		if err != nil {
			errs = append(errs, fmt.Errorf("%sREQUIRE_TOKEN_FOR_READS: %w", EnvPrefix, err))
		} else {
			o.RequireTokenForReads = b
			o.Sources["require_token_for_reads"] = "env"
		}
	}
	if v, ok := get("METRICS_TOKEN_REQUIRED"); ok {
		b, err := parseBool(v)
		if err != nil {
			errs = append(errs, fmt.Errorf("%sMETRICS_TOKEN_REQUIRED: %w", EnvPrefix, err))
		} else {
			o.MetricsTokenRequired = b
			o.Sources["metrics_token_required"] = "env"
		}
	}
	if v, ok := get("API_PORT"); ok {
		p, err := parsePort(v)
		if err != nil {
			errs = append(errs, fmt.Errorf("%sAPI_PORT: %w", EnvPrefix, err))
		} else {
			o.APIPort = p
			o.Sources["api_port"] = "env"
		}
	}
	if v, ok := get("API_TOKEN"); ok {
		o.APIToken = v
		o.Sources["api_token"] = "env"
	}
	if v, ok := get("METRICS_PORT"); ok {
		p, err := parsePort(v)
		if err != nil {
			errs = append(errs, fmt.Errorf("%sMETRICS_PORT: %w", EnvPrefix, err))
		} else {
			o.MetricsPort = p
			o.Sources["metrics_port"] = "env"
		}
	}
	if v, ok := get("POLICY_DIR"); ok {
		o.PolicyDir = v
		o.Sources["policy_dir"] = "env"
	}
	if v, ok := get("QUEUE_MAXLEN"); ok {
		n, err := strconv.Atoi(strings.TrimSpace(v))
		if err != nil || n < 1 || n > 65535 {
			errs = append(errs, fmt.Errorf("%sQUEUE_MAXLEN: invalid queue length %q", EnvPrefix, v))
		} else {
			o.QueueMaxlen = n
			o.Sources["queue_maxlen"] = "env"
		}
	}
	if v, ok := get("HOOK_SCOPE"); ok {
		o.HookScope = v
		o.Sources["hook_scope"] = "env"
	}
	if v, ok := get("OFFLOAD_POLICY"); ok {
		o.OffloadPolicy = v
		o.Sources["offload_policy"] = "env"
	}
	if v, ok := get("QUEUE_NUM"); ok {
		n, err := strconv.Atoi(strings.TrimSpace(v))
		if err != nil || n < 0 || n > 65534 {
			errs = append(errs, fmt.Errorf("%sQUEUE_NUM: invalid queue number %q (0..65534; 65535 would overflow the IPv6 queue+1 rule)", EnvPrefix, v))
		} else {
			o.QueueNum = n
			o.Sources["queue_num"] = "env"
		}
	}
	return errors.Join(errs...)
}

func (o *Options) loadOverrides(ov Overrides) {
	if ov.Interface != nil {
		o.Interface = *ov.Interface
		o.Sources["interface"] = "flag"
	}
	if ov.PolicyFile != nil {
		o.PolicyFile = *ov.PolicyFile
		o.Sources["policy_file"] = "flag"
	}
	if ov.LogLevel != nil {
		o.LogLevel = *ov.LogLevel
		o.Sources["log_level"] = "flag"
	}
	if ov.LogJSON != nil {
		o.LogJSON = *ov.LogJSON
		o.Sources["log_json"] = "flag"
	}
	if ov.APIHost != nil {
		o.APIHost = *ov.APIHost
		o.Sources["api_host"] = "flag"
	}
	if ov.AllowNonLoopbackAPIHost != nil {
		o.AllowNonLoopbackAPIHost = *ov.AllowNonLoopbackAPIHost
		o.Sources["allow_non_loopback_api_host"] = "flag"
	}
	if ov.RequireTokenForReads != nil {
		o.RequireTokenForReads = *ov.RequireTokenForReads
		o.Sources["require_token_for_reads"] = "flag"
	}
	if ov.MetricsTokenRequired != nil {
		o.MetricsTokenRequired = *ov.MetricsTokenRequired
		o.Sources["metrics_token_required"] = "flag"
	}
	if ov.APIPort != nil {
		o.APIPort = *ov.APIPort
		o.Sources["api_port"] = "flag"
	}
	if ov.MetricsPort != nil {
		o.MetricsPort = *ov.MetricsPort
		o.Sources["metrics_port"] = "flag"
	}
	if ov.PolicyDir != nil {
		o.PolicyDir = *ov.PolicyDir
		o.Sources["policy_dir"] = "flag"
	}
	if ov.QueueMaxlen != nil {
		o.QueueMaxlen = *ov.QueueMaxlen
		o.Sources["queue_maxlen"] = "flag"
	}
}

// Source reports which layer supplied a field's value.
func (o *Options) Source(field string) string {
	if s, ok := o.Sources[field]; ok {
		return s
	}
	return "unknown"
}

// SourceSummary renders "field=layer" pairs in a stable order, for logs and
// for `goshisen config show`. The token is never rendered (not even its
// source) to avoid leaking secret provenance in logs.
func (o *Options) SourceSummary() string {
	fields := []string{"interface", "policy_file", "log_level", "log_json", "api_host", "allow_non_loopback_api_host", "api_port", "metrics_port", "require_token_for_reads", "metrics_token_required", "policy_dir", "queue_maxlen", "queue_num", "hook_scope"}
	parts := make([]string, 0, len(fields))
	for _, f := range fields {
		parts = append(parts, f+"="+o.Source(f))
	}
	return strings.Join(parts, " ")
}

// validate checks resolved values. It performs no system calls.
func (o *Options) validate() error {
	var errs []error
	if o.PolicyFile == "" {
		errs = append(errs, fmt.Errorf("policy_file is empty (set it with --policy-file, %sPOLICY_FILE, or in the config file)", EnvPrefix))
	}
	if !isValidLogLevel(o.LogLevel) {
		errs = append(errs, fmt.Errorf("log_level %q is not one of debug, info, warn, error", o.LogLevel))
	}
	if strings.ContainsAny(o.Interface, " \t\n") {
		errs = append(errs, fmt.Errorf("interface %q must not contain whitespace", o.Interface))
	}
	if o.APIPort < 0 || o.APIPort > 65535 {
		errs = append(errs, fmt.Errorf("api_port %d out of range 0..65535", o.APIPort))
	}
	if o.MetricsPort < 0 || o.MetricsPort > 65535 {
		errs = append(errs, fmt.Errorf("metrics_port %d out of range 0..65535", o.MetricsPort))
	}
	if o.APIPort != 0 && o.APIPort == o.MetricsPort {
		errs = append(errs, fmt.Errorf("api_port and metrics_port must differ (both %d)", o.APIPort))
	}
	// The D1 hardening opt-ins only mean something with a token to check
	// against; refuse a configuration that claims protection it cannot
	// provide (fail-closed rather than a silent no-op).
	if o.RequireTokenForReads && o.APIToken == "" {
		errs = append(errs, errors.New("require_token_for_reads is set but api_token is empty; set api_token (config file) or GOSHISEN_API_TOKEN (environment), or drop the opt-in"))
	}
	if o.MetricsTokenRequired && o.APIToken == "" {
		errs = append(errs, errors.New("metrics_token_required is set but api_token is empty; set api_token (config file) or GOSHISEN_API_TOKEN (environment), or drop the opt-in"))
	}
	if o.APIHost != "" && strings.ContainsAny(o.APIHost, " \t\n") {
		errs = append(errs, fmt.Errorf("api_host %q must not contain whitespace", o.APIHost))
	}
	if !o.AllowNonLoopbackAPIHost && !isLoopbackAPIHost(o.APIHost) {
		errs = append(errs, fmt.Errorf(
			"api_host %q is not loopback; the API is bound to the network unless you opt in with allow_non_loopback_api_host: true (config file), %sALLOW_NON_LOOPBACK_API_HOST=1 (environment), or --allow-non-loopback-api-host (flag)",
			o.APIHost, EnvPrefix))
	}
	if o.PolicyDir == "" {
		errs = append(errs, fmt.Errorf("policy_dir is empty (set it with %sPOLICY_DIR, or in the config file)", EnvPrefix))
	}
	if o.QueueMaxlen < 1 || o.QueueMaxlen > 65535 {
		errs = append(errs, fmt.Errorf("queue_maxlen %d out of range 1..65535", o.QueueMaxlen))
	}
	if o.QueueNum < 0 || o.QueueNum > 65534 {
		errs = append(errs, fmt.Errorf("queue_num %d out of range 0..65534 (65535 reserved: the hook installs an IPv6 queue rule at queue_num+1)", o.QueueNum))
	}
	if o.HookScope != "" && o.HookScope != HookScopeForwardAll && o.HookScope != HookScopeOpenWrt {
		errs = append(errs, fmt.Errorf("hook_scope %q must be %q or %q",
			o.HookScope, HookScopeForwardAll, HookScopeOpenWrt))
	}
	if o.HookScope == HookScopeOpenWrt && o.Nat.Enabled {
		errs = append(errs, errors.New("nat is not supported with hook_scope openwrt (firewall4 owns NAT)"))
	}
	switch o.OffloadPolicy {
	case OffloadPolicyWarn, OffloadPolicyRefuse, OffloadPolicyReadiness, OffloadPolicyEnforce:
	default:
		errs = append(errs, fmt.Errorf("offload_policy %q must be warn, refuse, readiness, or enforce", o.OffloadPolicy))
	}
	if o.OffloadPolicy != OffloadPolicyEnforce && o.AllowBypass {
		errs = append(errs, errors.New("allow_bypass is only valid with offload_policy=enforce"))
	}
	if o.Nat.Enabled {
		if o.Nat.Masquerade.Enabled {
			if o.Nat.Masquerade.OutIface == "" {
				errs = append(errs, errors.New("nat: masquerade.out_iface is required when masquerade is enabled"))
			}
		}
		seenNames := map[string]bool{}
		for i, d := range o.Nat.Dnat {
			if d.Name == "" {
				errs = append(errs, fmt.Errorf("nat: dnat[%d].name is required", i))
			} else if seenNames[d.Name] {
				errs = append(errs, fmt.Errorf("nat: duplicate dnat name %q", d.Name))
			} else {
				seenNames[d.Name] = true
			}
			if d.Protocol != "tcp" && d.Protocol != "udp" {
				errs = append(errs, fmt.Errorf("nat: dnat[%d] protocol %q must be tcp or udp", i, d.Protocol))
			}
			if a, err := netip.ParseAddr(d.PublicIP); err != nil || !a.Is4() {
				errs = append(errs, fmt.Errorf("nat: dnat[%d] pub_ip %q must be an IPv4 address", i, d.PublicIP))
			}
			if a, err := netip.ParseAddr(d.TargetIP); err != nil || !a.Is4() {
				errs = append(errs, fmt.Errorf("nat: dnat[%d] target_ip %q must be an IPv4 address", i, d.TargetIP))
			}
			if d.PublicPort < 1 || d.PublicPort > 65535 {
				errs = append(errs, fmt.Errorf("nat: dnat[%d] pub_port %d out of range 1..65535", i, d.PublicPort))
			}
			if d.TargetPort < 1 || d.TargetPort > 65535 {
				errs = append(errs, fmt.Errorf("nat: dnat[%d] target_port %d out of range 1..65535", i, d.TargetPort))
			}
		}
	}
	return errors.Join(errs...)
}

func isValidLogLevel(s string) bool {
	switch s {
	case "debug", "info", "warn", "error":
		return true
	}
	return false
}

// isLoopbackAPIHost reports whether host is a loopback address or the
// loopback hostname. Accepted: empty (means the default 127.0.0.1),
// any 127.0.0.0/8 address, ::1, and "localhost" (case-insensitive).
// Anything else (another IP literal or a hostname) is not loopback; no
// DNS resolution is performed.
func isLoopbackAPIHost(host string) bool {
	if host == "" {
		return true
	}
	if strings.EqualFold(host, "localhost") {
		return true
	}
	addr, err := netip.ParseAddr(host)
	if err != nil {
		// Not an IP literal. A hostname is not loopback (no DNS).
		return false
	}
	if addr.Is4() {
		return addr.As4()[0] == 127
	}
	return addr.IsLoopback()
}

// Check performs read-only validation against the host: the policy file
// must be readable and the interface (explicit or auto-detected) must
// exist and be up. It never changes system state.
func (o *Options) Check() error {
	var errs []error
	if p := ExpandPath(o.PolicyFile); p != "" {
		f, err := os.Open(p)
		if err != nil {
			errs = append(errs, fmt.Errorf("policy_file: %w", err))
		} else {
			_ = f.Close()
		}
	}
	if _, err := o.InterfaceName(); err != nil {
		errs = append(errs, err)
	}
	return errors.Join(errs...)
}

// InterfaceName returns the configured interface, or the auto-detected
// one when unset. It is read-only.
func (o *Options) InterfaceName() (string, error) {
	if o.Interface != "" {
		ifi, err := net.InterfaceByName(o.Interface)
		if err != nil {
			return "", fmt.Errorf("interface %q: %w", o.Interface, err)
		}
		if ifi.Flags&net.FlagUp == 0 {
			return "", fmt.Errorf("interface %q is down", o.Interface)
		}
		return ifi.Name, nil
	}
	ifis, err := net.Interfaces()
	if err != nil {
		return "", fmt.Errorf("list interfaces: %w", err)
	}
	for _, ifi := range ifis {
		if ifi.Flags&net.FlagUp != 0 && ifi.Flags&net.FlagLoopback == 0 {
			return ifi.Name, nil
		}
	}
	return "", errors.New("no up, non-loopback interface found; set interface explicitly")
}

// ExpandPath expands a leading "~" using the user's home directory. Other
// forms are returned unchanged.
func ExpandPath(p string) string {
	if !strings.HasPrefix(p, "~/") {
		return p
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return p
	}
	return filepath.Join(home, p[2:])
}

func parseBool(s string) (bool, error) {
	switch strings.ToLower(strings.TrimSpace(s)) {
	case "1", "true", "yes", "on":
		return true, nil
	case "0", "false", "no", "off":
		return false, nil
	}
	return false, fmt.Errorf("invalid boolean %q (want true/false, 1/0, yes/no, on/off)", s)
}

func parsePort(s string) (int, error) {
	n, err := strconv.Atoi(strings.TrimSpace(s))
	if err != nil || n < 0 || n > 65535 {
		return 0, fmt.Errorf("invalid port %q (0..65535)", s)
	}
	return n, nil
}

// RedactedValue masks secrets for display.
func RedactedValue(s string) string {
	if s == "" {
		return ""
	}
	return "***"
}
