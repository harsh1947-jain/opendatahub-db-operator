/*
Copyright 2026.

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

    http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.
*/

package postgres

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	. "github.com/onsi/gomega"
)

func TestSQLExtensionAvailable_ReferencesCatalog(t *testing.T) {
	g := NewWithT(t)
	g.Expect(sqlExtensionAvailable).To(ContainSubstring("pg_available_extensions"))
	g.Expect(strings.Contains(sqlExtensionAvailable, "$1")).To(BeTrue())
}

func TestExtensionAvailable_Stub(t *testing.T) {
	t.Run("available", func(t *testing.T) {
		g := NewWithT(t)
		cli := &extensionStubClient{available: true}
		ok, err := ExtensionAvailable(context.Background(), cli, "vector")
		g.Expect(err).NotTo(HaveOccurred())
		g.Expect(ok).To(BeTrue())
		g.Expect(cli.lastSQL).To(ContainSubstring("pg_available_extensions"))
		g.Expect(cli.lastArgs).To(Equal([]any{"vector"}))
	})

	t.Run("absent", func(t *testing.T) {
		g := NewWithT(t)
		cli := &extensionStubClient{available: false}
		ok, err := ExtensionAvailable(context.Background(), cli, "vector")
		g.Expect(err).NotTo(HaveOccurred())
		g.Expect(ok).To(BeFalse())
	})

	t.Run("query error", func(t *testing.T) {
		g := NewWithT(t)
		cli := &extensionStubClient{queryErr: errors.New("boom")}
		ok, err := ExtensionAvailable(context.Background(), cli, "vector")
		g.Expect(ok).To(BeFalse())
		g.Expect(err).To(MatchError(ContainSubstring("checking extension \"vector\" availability")))
		g.Expect(err).To(MatchError(ContainSubstring("boom")))
	})
}

func TestMissingExtensions_Stub(t *testing.T) {
	g := NewWithT(t)
	cli := &extensionStubClient{availableByName: map[string]bool{"vector": false}}
	missing, err := MissingExtensions(context.Background(), cli, []string{"vector"})
	g.Expect(err).NotTo(HaveOccurred())
	g.Expect(missing).To(Equal([]string{"vector"}))
}

type extensionStubClient struct {
	available       bool
	availableByName map[string]bool
	queryErr        error
	lastSQL         string
	lastArgs        []any
}

func (c *extensionStubClient) Config() Config { return Config{} }
func (c *extensionStubClient) Close()         {}
func (c *extensionStubClient) Ping(context.Context) error {
	return nil
}
func (c *extensionStubClient) Exec(context.Context, string, ...any) (pgconn.CommandTag, error) {
	return pgconn.CommandTag{}, nil
}
func (c *extensionStubClient) Query(context.Context, string, ...any) (pgx.Rows, error) {
	return nil, nil
}
func (c *extensionStubClient) QueryRow(_ context.Context, sql string, args ...any) (pgx.Row, error) {
	c.lastSQL = sql
	c.lastArgs = append([]any(nil), args...)
	if c.queryErr != nil {
		return nil, c.queryErr
	}
	available := c.available
	if c.availableByName != nil && len(args) > 0 {
		if name, ok := args[0].(string); ok {
			available = c.availableByName[name]
		}
	}
	return extensionStubRow{available: available}, nil
}

type extensionStubRow struct {
	available bool
}

func (r extensionStubRow) Scan(dest ...any) error {
	if len(dest) != 1 {
		return errors.New("expected one scan destination")
	}
	ptr, ok := dest[0].(*bool)
	if !ok {
		return errors.New("expected *bool scan destination")
	}
	*ptr = r.available
	return nil
}
