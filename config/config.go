// Config is put into a different package to prevent cyclic imports in case
// it is needed in several locations

// TODO: incorporate registry file for state maintenance.

package config

import "time"

// Config represents o356beat configuration options
type Config struct {
	Period           time.Duration `config:"period"`
	TenantDomain     string        `config:"tenant_domain"`
	ClientSecret     string        `config:"client_secret"`
	ClientID         string        `config:"client_id"`    // aka application id
	DirectoryID      string        `config:"directory_id"` // aka tenant id
	ContentTypes     []string      `config:"content_types"`
	RegistryFilePath string        `config:"registry_file_path"`
	// LoginUrl     string        `config:"login_url"`
	// ResourceUrl  string        `config:"resource_url"`
}

// DefaultConfig sets defaults for configuration options
var DefaultConfig = Config{
	Period:           60 * 5 * time.Second, // TODO: tune this default, start at 5 min
	RegistryFilePath: "./o365beat-registry.json",
}
