package cmd

import (
	"context"
	"fmt"
	"os"
	"sync"

	"github.com/urfave/cli/v3"
)

// configPath holds the value of the global --config flag. It is written during
// flag parsing and read lazily by resolveConfigPath, so the ordering between
// the --config flag and the flags that read from the config file does not
// matter.
var configPath string

// configEnvVar is the environment variable used to locate the config file when
// --config is not given. It must match the --config flag defined in BuildApp.
const configEnvVar = "IMAP_BACKUP_TOOL_CONFIG"

// defaultConfigPath is used when neither --config nor configEnvVar is set.
const defaultConfigPath = "imap-backup-tool.yaml"

// resolveConfigPath returns the config file location at lookup time.
func resolveConfigPath() string {
	if configPath != "" {
		return configPath
	}
	if v := os.Getenv(configEnvVar); v != "" {
		return v
	}
	return defaultConfigPath
}

// loadedConfig caches the parsed config file. The file is read at most once per
// run, the first time a flag looks a value up or LoadedConfig is called.
var loadedConfig struct {
	once sync.Once
	file *ConfigFile
	err  error
}

// LoadedConfig returns the parsed config file, reading it on first use. A
// config file that is simply absent is not an error: the returned ConfigFile is
// nil and every lookup misses, leaving flags to fall back to environment
// variables and defaults. A file that exists but cannot be read, parsed,
// version-dispatched or validated returns an error.
func LoadedConfig() (*ConfigFile, error) {
	loadedConfig.once.Do(func() {
		path := resolveConfigPath()
		if path == "" {
			return
		}
		if _, err := os.Stat(path); err != nil {
			if os.IsNotExist(err) {
				// Only an explicitly requested config has to exist.
				if configPath != "" || os.Getenv(configEnvVar) != "" {
					loadedConfig.err = fmt.Errorf("config file %s does not exist", path)
				}
				return
			}
			loadedConfig.err = fmt.Errorf("reading config %s: %w", path, err)
			return
		}
		loadedConfig.file, loadedConfig.err = LoadConfigFile(path)
	})
	return loadedConfig.file, loadedConfig.err
}

// checkConfig is the root Before hook. It forces the config file to be read
// before any command runs so that a malformed or unsupported-version file fails
// loudly instead of silently sourcing no values.
func checkConfig(ctx context.Context, _ *cli.Command) (context.Context, error) {
	if _, err := LoadedConfig(); err != nil {
		return ctx, err
	}
	return ctx, nil
}

// configValueSource looks a logical key up in the loaded config file. The key
// is resolved by the config's own schema version through VersionedConfig.Lookup,
// so a version 2 file can store a value under a different field name and still
// satisfy the same flag.
type configValueSource struct {
	key string
}

func (s configValueSource) Lookup() (string, bool) {
	file, err := LoadedConfig()
	if err != nil || file == nil || file.Config == nil {
		return "", false
	}
	return file.Config.Lookup(s.key)
}

func (s configValueSource) String() string {
	return fmt.Sprintf("yaml config %q key %q", resolveConfigPath(), s.key)
}

func (s configValueSource) GoString() string {
	return fmt.Sprintf("configValueSource{key:%q}", s.key)
}

// flagSources builds the value source chain for a flag: environment variables
// first, then the config file at the given logical key. Values given on the
// command line always win, since urfave/cli only consults these sources for
// flags that were not set during parsing.
func flagSources(key string, envVars ...string) cli.ValueSourceChain {
	chain := cli.EnvVars(envVars...)
	chain.Append(cli.NewValueSourceChain(configValueSource{key: key}))
	return chain
}
