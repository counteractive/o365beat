// Config is put into a different package to prevent cyclic imports in case
// it is needed in several locations

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
	APITimeout       time.Duration `config:"api_timeout"`
	ContentMaxAge    time.Duration `config:"content_max_age"`
	LoginURL         string        `config:"login_url"`
	ResourceURL      string        `config:"resource_url"`
}

// DefaultConfig sets defaults for configuration options (tune as necessary)
var DefaultConfig = Config{
	Period:           60 * 5 * time.Second,
	RegistryFilePath: "./o365beat.state",
	APITimeout:       30 * time.Second,
	ContentMaxAge:    (7 * 24 * 60) * time.Minute,
	LoginURL:         "https://login.microsoftonline.com",
	ResourceURL:      "https://manage.office.com",
}
