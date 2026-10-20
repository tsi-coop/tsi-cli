package render

import (
	"fmt"
	"net/url"
	"strconv"

	"github.com/tsi-coop/tsi-cli/config"
)

// externalHostPort extracts the port from the external admin DSN.
func externalHostPort(cfg *config.Config) (string, int, error) {
	u, err := url.Parse(cfg.Database.DSN)
	if err != nil {
		return "", 0, fmt.Errorf("database.dsn: %w", err)
	}
	port := 5432
	if p := u.Port(); p != "" {
		port, err = strconv.Atoi(p)
		if err != nil {
			return "", 0, fmt.Errorf("database.dsn port: %w", err)
		}
	}
	return u.Hostname(), port, nil
}

// AdminDSN is the connection string tsi uses from the host to create roles and databases.
func (l *Layout) AdminDSN(m *Module, adminPass string) string {
	switch l.Cfg.Database.Mode {
	case config.Shared:
		return fmt.Sprintf("postgres://%s:%s@127.0.0.1:%d/postgres?sslmode=disable",
			PGUser, url.QueryEscape(adminPass), l.Cfg.SharedDBPort())
	case config.PerModule:
		return m.ModuleDSN(m.DBHostPort, "127.0.0.1")
	default:
		return l.Cfg.Database.DSN
	}
}

// ModuleDSN connects as the module's own role to its own database.
func (m *Module) ModuleDSN(port int, host string) string {
	return fmt.Sprintf("postgres://%s:%s@%s:%d/%s?sslmode=disable",
		m.DBUser, url.QueryEscape(m.DBPass), host, port, m.DBName)
}

// HostModuleDSN is the module DSN as seen from the machine running tsi.
func (l *Layout) HostModuleDSN(m *Module) (string, error) {
	switch l.Cfg.Database.Mode {
	case config.Shared:
		return m.ModuleDSN(l.Cfg.SharedDBPort(), "127.0.0.1"), nil
	case config.PerModule:
		return m.ModuleDSN(m.DBHostPort, "127.0.0.1"), nil
	default:
		host, port, err := externalHostPort(l.Cfg)
		if err != nil {
			return "", err
		}
		return m.ModuleDSN(port, host), nil
	}
}
