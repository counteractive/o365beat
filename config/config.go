// Config is put into a different package to prevent cyclic imports in case
// it is needed in several locations

// TODO: incorporate "registry file" for state maintenance

package config

import "time"

type Config struct {
	Period       time.Duration `config:"period"`
	TenantDomain string        `config:"tenant_domain"`
	ClientSecret string        `config:"client_secret"`
	ClientId     string        `config:"client_id"`    // aka application id
	DirectoryId  string        `config:"directory_id"` // aka tenant id
	LoginUrl     string        `config:"login_url"`
	ResourceUrl  string        `config:"resource_url"`
}

var DefaultConfig = Config{
	Period: 1 * time.Second,
}
