// Package ardaconfig loads a service configuration struct from an optional
// YAML file followed by environment overrides.
//
// It replaces the per-service Load()/loadYAML()/envStr() boilerplate without
// changing which YAML keys or environment variables a service honours: both
// are declared explicitly on the struct, never derived from field names.
//
//	type Config struct {
//		AppName  string        `yaml:"app_name"  env:"APP_NAME"`
//		GRPCAddr string        `yaml:"-"         env:"GRPC_ADDR"`                    // env only
//		NATSURL  string        `yaml:"nats_url"  env:"NATS_URL,MEDIA_NATS_URL"`      // later alias wins
//		MaxMB    int64         `yaml:"max_mb"    env:"MAX_MB" config:"positive"`     // zero/negative ignored
//		TTL      time.Duration `yaml:"ttl"       env:"TTL"`                          // "15m"
//		Targets  map[string]string `yaml:"targets" env:"TARGETS"`                   // "a=1,b=2"
//	}
//
// Rules, chosen to match the loaders this package replaces:
//
//   - Defaults are whatever the caller put in dst before calling Load.
//   - A YAML key is applied only when present and of the field's type; a key
//     without a `yaml` tag (or tagged "-") is never read from the file.
//   - Environment variables are applied in tag order; an empty value never
//     overrides, so "unset" and "set to empty" behave the same.
//   - `config:"positive"` additionally ignores empty strings and lists and
//     numbers <= 0 from both sources.
//   - Wrong types and unparsable values are reported, not silently dropped.
//     Load keeps applying the remaining keys and returns every problem joined
//     into one error, so a caller may log it and still start with the rest.
//   - If dst implements PostLoader, PostLoad runs last and its error is joined.
//
// Supported field types: string, int, int64, float64, bool, time.Duration,
// []string and map[string]string.
package ardaconfig

import (
	"errors"
	"fmt"
	"os"
	"reflect"
	"strconv"
	"strings"
	"time"

	"gopkg.in/yaml.v3"
)

// Options selects where Load reads the YAML file from.
type Options struct {
	// File is an explicit configuration file. When set, SearchPaths are
	// ignored and a missing or unreadable file is an error.
	File string
	// SearchPaths are tried in order when File is empty; the first existing
	// path is read. No path existing is not an error.
	SearchPaths []string
}

// PostLoader lets a config struct derive or validate fields after the file
// and the environment were applied.
type PostLoader interface {
	PostLoad() error
}

// DefaultSearchPaths returns the conventional lookup order for a service:
// next to the binary, one level up (go run from cmd/), then /etc/arda.
func DefaultSearchPaths(service string) []string {
	return []string{"configs/config.yaml", "../configs/config.yaml", "/etc/arda/" + service + "/config.yaml"}
}

// ServiceOptions is the conventional Options for a service: an explicit file
// from CONFIG_FILE when set, else DefaultSearchPaths.
func ServiceOptions(service string) Options {
	return Options{File: os.Getenv("CONFIG_FILE"), SearchPaths: DefaultSearchPaths(service)}
}

// Load fills dst (a pointer to a struct) from the YAML file selected by opts,
// then from the environment, then calls PostLoad when implemented. See the
// package documentation for the rules.
func Load(dst any, opts Options) error {
	fields, err := collect(dst)
	if err != nil {
		return err
	}
	var errs []error
	if path, ok, err := selectFile(opts); err != nil {
		errs = append(errs, err)
	} else if ok {
		if err := applyFile(path, fields); err != nil {
			errs = append(errs, err)
		}
	}
	if err := applyEnv(fields); err != nil {
		errs = append(errs, err)
	}
	if hook, ok := dst.(PostLoader); ok {
		if err := hook.PostLoad(); err != nil {
			errs = append(errs, fmt.Errorf("config: post-load: %w", err))
		}
	}
	return errors.Join(errs...)
}

// LoadFile applies only the YAML file at path to dst.
func LoadFile(path string, dst any) error {
	fields, err := collect(dst)
	if err != nil {
		return err
	}
	return applyFile(path, fields)
}

// ApplyEnv applies only the environment to dst.
func ApplyEnv(dst any) error {
	fields, err := collect(dst)
	if err != nil {
		return err
	}
	return applyEnv(fields)
}

func selectFile(opts Options) (string, bool, error) {
	if opts.File != "" {
		if _, err := os.Stat(opts.File); err != nil {
			return "", false, fmt.Errorf("config: file %s: %w", opts.File, err)
		}
		return opts.File, true, nil
	}
	for _, path := range opts.SearchPaths {
		if info, err := os.Stat(path); err == nil && !info.IsDir() {
			return path, true, nil
		}
	}
	return "", false, nil
}

type field struct {
	name     string
	yamlKey  string
	envNames []string
	positive bool
	value    reflect.Value
}

func collect(dst any) ([]field, error) {
	rv := reflect.ValueOf(dst)
	if rv.Kind() != reflect.Pointer || rv.IsNil() || rv.Elem().Kind() != reflect.Struct {
		return nil, fmt.Errorf("config: destination must be a non-nil pointer to a struct, got %T", dst)
	}
	rv = rv.Elem()
	rt := rv.Type()
	var fields []field
	for i := 0; i < rt.NumField(); i++ {
		sf := rt.Field(i)
		if !sf.IsExported() {
			continue
		}
		f := field{name: sf.Name, value: rv.Field(i)}
		if tag := sf.Tag.Get("yaml"); tag != "" && tag != "-" {
			f.yamlKey, _, _ = strings.Cut(tag, ",")
		}
		if tag := sf.Tag.Get("env"); tag != "" && tag != "-" {
			for _, name := range strings.Split(tag, ",") {
				if name = strings.TrimSpace(name); name != "" {
					f.envNames = append(f.envNames, name)
				}
			}
		}
		if f.yamlKey == "" && len(f.envNames) == 0 {
			continue
		}
		switch tag := sf.Tag.Get("config"); tag {
		case "":
		case "positive":
			f.positive = true
		default:
			return nil, fmt.Errorf("config: field %s: unknown config tag %q", sf.Name, tag)
		}
		if !supported(sf.Type) {
			return nil, fmt.Errorf("config: field %s: unsupported type %s", sf.Name, sf.Type)
		}
		fields = append(fields, f)
	}
	return fields, nil
}

var (
	durationType  = reflect.TypeOf(time.Duration(0))
	stringSlice   = reflect.TypeOf([]string(nil))
	stringMapType = reflect.TypeOf(map[string]string(nil))
)

func supported(t reflect.Type) bool {
	if t == durationType || t == stringSlice || t == stringMapType {
		return true
	}
	switch t.Kind() {
	case reflect.String, reflect.Int, reflect.Int64, reflect.Float64, reflect.Bool:
		return true
	}
	return false
}

func applyFile(path string, fields []field) error {
	data, err := os.ReadFile(path)
	if err != nil {
		return fmt.Errorf("config: read %s: %w", path, err)
	}
	raw := map[string]any{}
	if err := yaml.Unmarshal(data, &raw); err != nil {
		return fmt.Errorf("config: parse %s: %w", path, err)
	}
	var errs []error
	for _, f := range fields {
		if f.yamlKey == "" {
			continue
		}
		v, ok := raw[f.yamlKey]
		if !ok || v == nil {
			continue
		}
		if err := f.setYAML(v); err != nil {
			errs = append(errs, fmt.Errorf("config: %s: key %s: %w", path, f.yamlKey, err))
		}
	}
	return errors.Join(errs...)
}

func applyEnv(fields []field) error {
	var errs []error
	for _, f := range fields {
		for _, name := range f.envNames {
			raw := os.Getenv(name)
			if raw == "" {
				continue
			}
			if err := f.setEnv(raw); err != nil {
				errs = append(errs, fmt.Errorf("config: env %s: %w", name, err))
			}
		}
	}
	return errors.Join(errs...)
}

func (f field) setYAML(v any) error {
	t := f.value.Type()
	switch {
	case t == durationType:
		s, ok := v.(string)
		if !ok {
			return fmt.Errorf("expected a duration string, got %T", v)
		}
		d, err := time.ParseDuration(s)
		if err != nil {
			return err
		}
		return f.assign(reflect.ValueOf(d))
	case t == stringSlice:
		items, ok := v.([]any)
		if !ok {
			return fmt.Errorf("expected a list of strings, got %T", v)
		}
		out := make([]string, 0, len(items))
		for _, item := range items {
			s, ok := item.(string)
			if !ok {
				return fmt.Errorf("expected a list of strings, got element %T", item)
			}
			out = append(out, s)
		}
		return f.assign(reflect.ValueOf(out))
	case t == stringMapType:
		entries, ok := v.(map[string]any)
		if !ok {
			return fmt.Errorf("expected a map of strings, got %T", v)
		}
		out := make(map[string]string, len(entries))
		for k, item := range entries {
			s, ok := item.(string)
			if !ok {
				return fmt.Errorf("expected a map of strings, got value %T for %q", item, k)
			}
			out[k] = s
		}
		return f.assign(reflect.ValueOf(out))
	}
	switch t.Kind() {
	case reflect.String:
		s, ok := v.(string)
		if !ok {
			return fmt.Errorf("expected a string, got %T", v)
		}
		return f.assign(reflect.ValueOf(s))
	case reflect.Int, reflect.Int64:
		var n int64
		switch x := v.(type) {
		case int:
			n = int64(x)
		case int64:
			n = x
		default:
			return fmt.Errorf("expected an integer, got %T", v)
		}
		return f.assign(reflect.ValueOf(n).Convert(t))
	case reflect.Float64:
		var x float64
		switch y := v.(type) {
		case float64:
			x = y
		case int:
			x = float64(y)
		case int64:
			x = float64(y)
		default:
			return fmt.Errorf("expected a number, got %T", v)
		}
		return f.assign(reflect.ValueOf(x))
	case reflect.Bool:
		b, ok := v.(bool)
		if !ok {
			return fmt.Errorf("expected a boolean, got %T", v)
		}
		return f.assign(reflect.ValueOf(b))
	}
	return fmt.Errorf("unsupported type %s", t)
}

func (f field) setEnv(raw string) error {
	t := f.value.Type()
	switch {
	case t == durationType:
		d, err := time.ParseDuration(raw)
		if err != nil {
			return err
		}
		return f.assign(reflect.ValueOf(d))
	case t == stringSlice:
		var out []string
		for _, item := range strings.Split(raw, ",") {
			if s := strings.TrimSpace(item); s != "" {
				out = append(out, s)
			}
		}
		if len(out) == 0 {
			// A value made only of separators and blanks carries no items;
			// treat it like an empty variable.
			return nil
		}
		return f.assign(reflect.ValueOf(out))
	case t == stringMapType:
		out := map[string]string{}
		var errs []error
		for _, pair := range strings.Split(raw, ",") {
			pair = strings.TrimSpace(pair)
			if pair == "" {
				continue
			}
			k, v, ok := strings.Cut(pair, "=")
			k, v = strings.TrimSpace(k), strings.TrimSpace(v)
			if !ok || k == "" || v == "" {
				errs = append(errs, fmt.Errorf("malformed pair %q (want key=value)", pair))
				continue
			}
			out[k] = v
		}
		if err := f.assign(reflect.ValueOf(out)); err != nil {
			errs = append(errs, err)
		}
		return errors.Join(errs...)
	}
	switch t.Kind() {
	case reflect.String:
		return f.assign(reflect.ValueOf(raw))
	case reflect.Int, reflect.Int64:
		n, err := strconv.ParseInt(strings.TrimSpace(raw), 10, 64)
		if err != nil {
			return fmt.Errorf("expected an integer, got %q", raw)
		}
		return f.assign(reflect.ValueOf(n).Convert(t))
	case reflect.Float64:
		x, err := strconv.ParseFloat(strings.TrimSpace(raw), 64)
		if err != nil {
			return fmt.Errorf("expected a number, got %q", raw)
		}
		return f.assign(reflect.ValueOf(x))
	case reflect.Bool:
		b, err := strconv.ParseBool(strings.TrimSpace(raw))
		if err != nil {
			return fmt.Errorf("expected a boolean, got %q", raw)
		}
		return f.assign(reflect.ValueOf(b))
	}
	return fmt.Errorf("unsupported type %s", t)
}

// assign stores v unless the field is tagged positive and v is empty or
// non-positive.
func (f field) assign(v reflect.Value) error {
	if f.positive {
		switch v.Kind() {
		case reflect.String, reflect.Slice, reflect.Map:
			if v.Len() == 0 {
				return nil
			}
		case reflect.Int, reflect.Int64:
			if v.Int() <= 0 {
				return nil
			}
		case reflect.Float64:
			if v.Float() <= 0 {
				return nil
			}
		}
	}
	f.value.Set(v)
	return nil
}
