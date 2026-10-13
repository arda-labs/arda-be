package ardaconfig

import (
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"
)

type sample struct {
	AppName   string            `yaml:"app_name" env:"APP_NAME"`
	GRPCAddr  string            `yaml:"-" env:"GRPC_ADDR"`
	NATSURL   string            `yaml:"nats_url" env:"NATS_URL,SAMPLE_NATS_URL"`
	Workers   int               `yaml:"workers" env:"WORKERS"`
	MaxMB     int64             `yaml:"max_mb" env:"MAX_MB" config:"positive"`
	Ratio     float64           `yaml:"ratio" env:"RATIO"`
	Secure    bool              `yaml:"secure" env:"SECURE"`
	TTL       time.Duration     `yaml:"ttl" env:"TTL"`
	MIME      []string          `yaml:"mime" env:"MIME" config:"positive"`
	Targets   map[string]string `yaml:"targets" env:"TARGETS"`
	YAMLOnly  string            `yaml:"yaml_only"`
	Untagged  string
	unexpored string
}

func defaults() sample {
	return sample{AppName: "sample", GRPCAddr: "0.0.0.0:9090", NATSURL: "nats://localhost:4222", Workers: 4, MaxMB: 100, Ratio: 0.5, Secure: true, TTL: 15 * time.Minute, Untagged: "keep", unexpored: "keep"}
}

func writeYAML(t *testing.T, body string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "config.yaml")
	if err := os.WriteFile(path, []byte(body), 0o600); err != nil {
		t.Fatal(err)
	}
	return path
}

func clearEnv(t *testing.T) {
	t.Helper()
	for _, name := range []string{"APP_NAME", "GRPC_ADDR", "NATS_URL", "SAMPLE_NATS_URL", "WORKERS", "MAX_MB", "RATIO", "SECURE", "TTL", "MIME", "TARGETS"} {
		t.Setenv(name, "")
	}
}

func TestLoadTable(t *testing.T) {
	cases := []struct {
		name    string
		yaml    string // "" means no file
		env     map[string]string
		want    func(s *sample)
		wantErr []string // substrings every error must contain; nil means no error
	}{
		{
			name: "no file and no env keeps defaults",
			want: func(*sample) {},
		},
		{
			name: "yaml applies typed keys and skips untagged, dash and null",
			yaml: "app_name: from-yaml\nworkers: 8\nmax_mb: 200\nratio: 0.75\nsecure: false\nttl: 30m\nmime: [a/b, c/d]\ntargets: {x: \"http://x\"}\nyaml_only: y\nuntagged: nope\ngrpc_addr: nope\nnats_url:\n",
			want: func(s *sample) {
				s.AppName, s.Workers, s.MaxMB, s.Ratio, s.Secure, s.TTL = "from-yaml", 8, 200, 0.75, false, 30*time.Minute
				s.MIME = []string{"a/b", "c/d"}
				s.Targets = map[string]string{"x": "http://x"}
				s.YAMLOnly = "y"
			},
		},
		{
			name: "env overrides yaml and empty env does not",
			yaml: "app_name: from-yaml\nworkers: 8\n",
			env:  map[string]string{"APP_NAME": "from-env", "WORKERS": "", "GRPC_ADDR": "127.0.0.1:1", "TTL": "1h", "SECURE": "false", "RATIO": "2", "MIME": " x/y , ,z/w", "TARGETS": "a=1, b = 2"},
			want: func(s *sample) {
				s.AppName, s.Workers, s.GRPCAddr, s.TTL, s.Secure, s.Ratio = "from-env", 8, "127.0.0.1:1", time.Hour, false, 2
				s.MIME = []string{"x/y", "z/w"}
				s.Targets = map[string]string{"a": "1", "b": "2"}
			},
		},
		{
			name: "later env alias wins",
			env:  map[string]string{"NATS_URL": "first", "SAMPLE_NATS_URL": "second"},
			want: func(s *sample) { s.NATSURL = "second" },
		},
		{
			name: "earlier alias applies when the later one is empty",
			env:  map[string]string{"NATS_URL": "first", "SAMPLE_NATS_URL": ""},
			want: func(s *sample) { s.NATSURL = "first" },
		},
		{
			name: "positive ignores zero negative and empty",
			yaml: "max_mb: 0\nmime: []\n",
			env:  map[string]string{"MAX_MB": "-5", "MIME": " , "},
			want: func(*sample) {},
		},
		{
			name: "positive accepts a positive value",
			env:  map[string]string{"MAX_MB": "7"},
			want: func(s *sample) { s.MaxMB = 7 },
		},
		{
			name:    "yaml wrong type is reported and the rest still applies",
			yaml:    "app_name: 123\nworkers: eight\nsecure: yes-please\nttl: 10\nmime: a/b\ntargets: [a]\nratio: x\ngrpc_addr: 1\nnats_url: ok\n",
			want:    func(s *sample) { s.NATSURL = "ok" },
			wantErr: []string{"key app_name: expected a string, got int", "key workers: expected an integer", "key secure: expected a boolean", "key ttl: expected a duration string", "key mime: expected a list", "key targets: expected a map", "key ratio: expected a number"},
		},
		{
			name:    "env wrong type is reported and the rest still applies",
			env:     map[string]string{"WORKERS": "eight", "SECURE": "maybe", "TTL": "soon", "RATIO": "x", "TARGETS": "ok=1,broken,=v", "APP_NAME": "still"},
			want:    func(s *sample) { s.AppName = "still"; s.Targets = map[string]string{"ok": "1"} },
			wantErr: []string{"env WORKERS: expected an integer, got \"eight\"", "env SECURE: expected a boolean", "env TTL:", "env RATIO: expected a number", "env TARGETS: malformed pair \"broken\"", "malformed pair \"=v\""},
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			clearEnv(t)
			for k, v := range tc.env {
				t.Setenv(k, v)
			}
			opts := Options{}
			if tc.yaml != "" {
				opts.File = writeYAML(t, tc.yaml)
			}
			got := defaults()
			err := Load(&got, opts)
			if tc.wantErr == nil && err != nil {
				t.Fatalf("Load() error = %v", err)
			}
			for _, fragment := range tc.wantErr {
				if err == nil || !strings.Contains(err.Error(), fragment) {
					t.Fatalf("Load() error = %v, want it to mention %q", err, fragment)
				}
			}
			want := defaults()
			tc.want(&want)
			if !reflect.DeepEqual(got, want) {
				t.Fatalf("Load() = %+v, want %+v", got, want)
			}
		})
	}
}

func TestLoadExplicitFileMustExist(t *testing.T) {
	clearEnv(t)
	got := defaults()
	err := Load(&got, Options{File: filepath.Join(t.TempDir(), "missing.yaml")})
	if !errors.Is(err, fs.ErrNotExist) {
		t.Fatalf("Load() error = %v, want fs.ErrNotExist", err)
	}
	if !reflect.DeepEqual(got, defaults()) {
		t.Fatalf("missing file changed the config: %+v", got)
	}
}

func TestLoadInvalidYAMLIsReported(t *testing.T) {
	clearEnv(t)
	got := defaults()
	err := Load(&got, Options{File: writeYAML(t, "app_name: [unterminated\n")})
	if err == nil || !strings.Contains(err.Error(), "parse") {
		t.Fatalf("Load() error = %v, want a parse error", err)
	}
}

func TestLoadSearchPathsUseFirstExistingAndToleratesNone(t *testing.T) {
	clearEnv(t)
	dir := t.TempDir()
	second := filepath.Join(dir, "second.yaml")
	if err := os.WriteFile(second, []byte("app_name: second\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	third := filepath.Join(dir, "third.yaml")
	if err := os.WriteFile(third, []byte("app_name: third\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	got := defaults()
	if err := Load(&got, Options{SearchPaths: []string{filepath.Join(dir, "first.yaml"), second, third}}); err != nil {
		t.Fatal(err)
	}
	if got.AppName != "second" {
		t.Fatalf("AppName = %q, want the first existing search path", got.AppName)
	}
	got = defaults()
	if err := Load(&got, Options{SearchPaths: []string{filepath.Join(dir, "none.yaml")}}); err != nil || !reflect.DeepEqual(got, defaults()) {
		t.Fatalf("no search path existing: err=%v cfg=%+v", err, got)
	}
}

func TestLoadExplicitFileWinsOverSearchPaths(t *testing.T) {
	clearEnv(t)
	explicit := writeYAML(t, "app_name: explicit\n")
	searched := writeYAML(t, "app_name: searched\n")
	got := defaults()
	if err := Load(&got, Options{File: explicit, SearchPaths: []string{searched}}); err != nil {
		t.Fatal(err)
	}
	if got.AppName != "explicit" {
		t.Fatalf("AppName = %q", got.AppName)
	}
}

type hooked struct {
	Raw    string `env:"RAW"`
	Parsed []string
	fail   bool
}

func (h *hooked) PostLoad() error {
	if h.fail {
		return errors.New("boom")
	}
	h.Parsed = strings.Split(h.Raw, ";")
	return nil
}

func TestLoadCallsPostLoadAfterEnv(t *testing.T) {
	t.Setenv("RAW", "a;b")
	got := &hooked{}
	if err := Load(got, Options{}); err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(got.Parsed, []string{"a", "b"}) {
		t.Fatalf("PostLoad did not see the env value: %+v", got)
	}
	failing := &hooked{fail: true}
	if err := Load(failing, Options{}); err == nil || !strings.Contains(err.Error(), "post-load: boom") {
		t.Fatalf("Load() error = %v, want the PostLoad error", err)
	}
}

func TestLoadRejectsBadDestinationsAndTags(t *testing.T) {
	var notPointer sample
	if err := Load(notPointer, Options{}); err == nil {
		t.Fatal("Load(non-pointer) succeeded")
	}
	var unsupported struct {
		When time.Time `env:"WHEN"`
	}
	if err := Load(&unsupported, Options{}); err == nil || !strings.Contains(err.Error(), "unsupported type") {
		t.Fatalf("Load(unsupported type) error = %v", err)
	}
	var badTag struct {
		N int `env:"N" config:"nonzero"`
	}
	if err := Load(&badTag, Options{}); err == nil || !strings.Contains(err.Error(), "unknown config tag") {
		t.Fatalf("Load(unknown config tag) error = %v", err)
	}
}

func TestDefaultSearchPathsAndServiceOptions(t *testing.T) {
	want := []string{"configs/config.yaml", "../configs/config.yaml", "/etc/arda/hrm-service/config.yaml"}
	if got := DefaultSearchPaths("hrm-service"); !reflect.DeepEqual(got, want) {
		t.Fatalf("DefaultSearchPaths = %v", got)
	}
	t.Setenv("CONFIG_FILE", "x.yaml")
	if got := ServiceOptions("hrm-service"); got.File != "x.yaml" || !reflect.DeepEqual(got.SearchPaths, want) {
		t.Fatalf("ServiceOptions = %+v", got)
	}
}
