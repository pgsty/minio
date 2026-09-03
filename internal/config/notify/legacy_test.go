// Copyright (c) 2015-2026 MinIO, Inc.
//
// This file is part of MinIO Object Storage stack
//
// This program is free software: you can redistribute it and/or modify
// it under the terms of the GNU Affero General Public License as published by
// the Free Software Foundation, either version 3 of the License, or
// (at your option) any later version.
//
// This program is distributed in the hope that it will be useful
// but WITHOUT ANY WARRANTY; without even the implied warranty of
// MERCHANTABILITY or FITNESS FOR A PARTICULAR PURPOSE.  See the
// GNU Affero General Public License for more details.
//
// You should have received a copy of the GNU Affero General Public License
// along with this program.  If not, see <http://www.gnu.org/licenses/>.

package notify

import (
	"errors"
	"strings"
	"testing"

	"github.com/minio/minio/internal/config"
	"github.com/minio/minio/internal/event/target"
	xnet "github.com/pgsty/silo-pkg/v3/net"
	"github.com/rabbitmq/amqp091-go"
)

func assertLegacyDatabaseTargetError(t *testing.T, err error, subsystem, name, key string, secrets ...string) {
	t.Helper()
	var targetErr *LegacyDatabaseTargetError
	if !errors.As(err, &targetErr) {
		t.Fatalf("error = %v, want *LegacyDatabaseTargetError", err)
	}
	msg := err.Error()
	for _, want := range []string{subsystem + config.SubSystemSeparator + name, key} {
		if !strings.Contains(msg, want) {
			t.Errorf("error %q does not contain %q", msg, want)
		}
	}
	for _, secret := range secrets {
		if secret != "" && strings.Contains(msg, secret) {
			t.Errorf("error leaks configuration value %q: %s", secret, msg)
		}
	}
}

// T5 (NATS): a config produced by the legacy migration must survive validation
// and round-trip back through the parser unchanged. Before the fix the
// migration wrote an env var name as a config key, so every migrated NATS
// target failed CheckValidKeys on the next config load.
func TestSetNotifyNATSRoundTrip(t *testing.T) {
	addr, err := xnet.ParseHost(testNATSAddr)
	if err != nil {
		t.Fatalf("ParseHost: %v", err)
	}

	args := target.NATSArgs{
		Enable:            true,
		Address:           *addr,
		Subject:           testNATSSubj,
		UserCredentials:   testCredsPath,
		NKeySeed:          testNKeyPath,
		TLSHandshakeFirst: true,
	}

	s := config.Config{config.NotifyNATSSubSys: map[string]config.KVS{}}
	if err := SetNotifyNATS(s, testTargetName, args); err != nil {
		t.Fatalf("SetNotifyNATS: %v", err)
	}

	if err := checkValidNotificationKeysForSubSys(config.NotifyNATSSubSys, s[config.NotifyNATSSubSys]); err != nil {
		t.Fatalf("migrated NATS config must pass key validation, got: %v", err)
	}

	targets, err := GetNotifyNATS(s[config.NotifyNATSSubSys], nil)
	if err != nil {
		t.Fatalf("GetNotifyNATS: %v", err)
	}
	got, ok := targets[testTargetName]
	if !ok {
		t.Fatalf("target %q missing after round trip: %v", testTargetName, targets)
	}
	if got.UserCredentials != args.UserCredentials {
		t.Errorf("UserCredentials = %q, want %q", got.UserCredentials, args.UserCredentials)
	}
	if got.NKeySeed != args.NKeySeed {
		t.Errorf("NKeySeed = %q, want %q", got.NKeySeed, args.NKeySeed)
	}
	if got.TLSHandshakeFirst != args.TLSHandshakeFirst {
		t.Errorf("TLSHandshakeFirst = %v, want %v", got.TLSHandshakeFirst, args.TLSHandshakeFirst)
	}
}

// T5 (AMQP): the migration mapped cfg.Immediate onto the `internal` key and
// dropped cfg.Internal entirely, so a migrated target came back with both
// fields wrong.
func TestSetNotifyAMQPRoundTrip(t *testing.T) {
	uri, err := amqp091.ParseURI(testAMQPURL)
	if err != nil {
		t.Fatalf("ParseURI: %v", err)
	}

	args := target.AMQPArgs{
		Enable:    true,
		URL:       uri,
		Immediate: true,
		Internal:  false,
	}

	s := config.Config{config.NotifyAMQPSubSys: map[string]config.KVS{}}
	if err := SetNotifyAMQP(s, testTargetName, args); err != nil {
		t.Fatalf("SetNotifyAMQP: %v", err)
	}

	if err := checkValidNotificationKeysForSubSys(config.NotifyAMQPSubSys, s[config.NotifyAMQPSubSys]); err != nil {
		t.Fatalf("migrated AMQP config must pass key validation, got: %v", err)
	}

	targets, err := GetNotifyAMQP(s[config.NotifyAMQPSubSys])
	if err != nil {
		t.Fatalf("GetNotifyAMQP: %v", err)
	}
	got, ok := targets[testTargetName]
	if !ok {
		t.Fatalf("target %q missing after round trip: %v", testTargetName, targets)
	}
	if !got.Immediate {
		t.Errorf("Immediate = false, want true")
	}
	if got.Internal {
		t.Errorf("Internal = true, want false (immediate must not be written to the internal key)")
	}
}

func TestSetNotifyDatabaseTargetsRequireConnectionStrings(t *testing.T) {
	postgresHost, err := xnet.ParseHost("legacy-postgres.example")
	if err != nil {
		t.Fatal(err)
	}
	mysqlHost, err := xnet.ParseURL("legacy-mysql.example")
	if err != nil {
		t.Fatal(err)
	}

	tests := []struct {
		name      string
		subsystem string
		key       string
		set       func(config.Config) error
		secrets   []string
	}{
		{
			name:      "postgres",
			subsystem: config.NotifyPostgresSubSys,
			key:       target.PostgresConnectionString,
			set: func(s config.Config) error {
				return SetNotifyPostgres(s, testTargetName, target.PostgreSQLArgs{
					Enable:   true,
					Format:   formatNamespace,
					Table:    "events",
					Host:     *postgresHost,
					Port:     "5432",
					Username: "legacy-user",
					Password: "legacy-postgres-password",
					Database: "legacy-database",
				})
			},
			secrets: []string{postgresHost.String(), "5432", "legacy-user", "legacy-postgres-password", "legacy-database"},
		},
		{
			name:      "mysql",
			subsystem: config.NotifyMySQLSubSys,
			key:       target.MySQLDSNString,
			set: func(s config.Config) error {
				return SetNotifyMySQL(s, testTargetName, target.MySQLArgs{
					Enable:   true,
					Format:   formatNamespace,
					Table:    "events",
					Host:     *mysqlHost,
					Port:     "3306",
					User:     "legacy-user",
					Password: "legacy-mysql-password",
					Database: "legacy-database",
				})
			},
			secrets: []string{mysqlHost.String(), "3306", "legacy-user", "legacy-mysql-password", "legacy-database"},
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			s := config.Config{test.subsystem: map[string]config.KVS{}}
			err := test.set(s)
			assertLegacyDatabaseTargetError(t, err, test.subsystem, testTargetName, test.key, test.secrets...)
			if _, ok := s[test.subsystem][testTargetName]; ok {
				t.Fatal("unsupported target was emitted despite migration error")
			}
		})
	}
}

func TestSetNotifyDisabledDatabaseTargetsAreIgnored(t *testing.T) {
	s := config.Config{
		config.NotifyPostgresSubSys: map[string]config.KVS{},
		config.NotifyMySQLSubSys:    map[string]config.KVS{},
	}
	if err := SetNotifyPostgres(s, testTargetName, target.PostgreSQLArgs{Password: "discarded-postgres-secret"}); err != nil {
		t.Fatalf("SetNotifyPostgres: %v", err)
	}
	if err := SetNotifyMySQL(s, testTargetName, target.MySQLArgs{Password: "discarded-mysql-secret"}); err != nil {
		t.Fatalf("SetNotifyMySQL: %v", err)
	}
	if _, ok := s[config.NotifyPostgresSubSys][testTargetName]; ok {
		t.Fatal("disabled Postgres target was emitted")
	}
	if _, ok := s[config.NotifyMySQLSubSys][testTargetName]; ok {
		t.Fatal("disabled MySQL target was emitted")
	}
}

func TestSetNotifyInvalidDatabaseTargetsDoNotLeak(t *testing.T) {
	tests := []struct {
		name      string
		subsystem string
		key       string
		secret    string
		set       func(config.Config) error
	}{
		{
			name:      "postgres",
			subsystem: config.NotifyPostgresSubSys,
			key:       target.PostgresConnectionString,
			secret:    "postgres-dsn-secret",
			set: func(s config.Config) error {
				return SetNotifyPostgres(s, testTargetName, target.PostgreSQLArgs{
					Enable:           true,
					Format:           formatNamespace,
					ConnectionString: "host=db password=postgres-dsn-secret",
				})
			},
		},
		{
			name:      "mysql",
			subsystem: config.NotifyMySQLSubSys,
			key:       target.MySQLDSNString,
			secret:    "mysql-dsn-secret",
			set: func(s config.Config) error {
				return SetNotifyMySQL(s, testTargetName, target.MySQLArgs{
					Enable: true,
					Format: formatNamespace,
					DSN:    "user:mysql-dsn-secret@tcp(db:3306/events",
					Table:  "events",
				})
			},
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			s := config.Config{test.subsystem: map[string]config.KVS{}}
			err := test.set(s)
			assertLegacyDatabaseTargetError(t, err, test.subsystem, testTargetName, test.key, test.secret)
		})
	}
}

func TestDatabaseConnectionStringsSurviveKVTokenization(t *testing.T) {
	tests := []struct {
		name      string
		subsystem string
		key       string
		input     string
		want      string
	}{
		{
			name:      "postgres",
			subsystem: config.NotifyPostgresSubSys,
			key:       target.PostgresConnectionString,
			input:     `notify_postgres:dsn connection_string="host=db port=5432 dbname=events user=app password=inside" table="events"`,
			want:      "host=db port=5432 dbname=events user=app password=inside",
		},
		{
			name:      "mysql",
			subsystem: config.NotifyMySQLSubSys,
			key:       target.MySQLDSNString,
			input:     `notify_mysql:dsn dsn_string="user:pass@tcp(db:3306)/events?host=db&port=3306&password=inside" table="events"`,
			want:      "user:pass@tcp(db:3306)/events?host=db&port=3306&password=inside",
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			s := config.Config{test.subsystem: map[string]config.KVS{}}
			if _, err := s.SetKVS(test.input, DefaultNotificationKVS); err != nil {
				t.Fatalf("SetKVS: %v", err)
			}
			if got := s[test.subsystem]["dsn"].Get(test.key); got != test.want {
				t.Errorf("%s = %q, want %q", test.key, got, test.want)
			}
		})
	}
}
