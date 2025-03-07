// Copyright 2022 Molecula Corp. (DBA FeatureBase).
// SPDX-License-Identifier: Apache-2.0
package test

import (
	"context"
	"testing"

	pilosa "github.com/featurebasedb/featurebase/v3"
	"github.com/featurebasedb/featurebase/v3/testhook"
)

// Index represents a test wrapper for pilosa.Index.
type Index struct {
	*pilosa.Index
}

// newIndex returns a new instance of Index, and the parent holder.
func newIndex(tb testing.TB) (*Holder, *Index) {
	h := NewHolder(tb)
	testhook.Cleanup(tb, func() {
		h.Close()
	})
	index, err := h.CreateIndex("i", pilosa.IndexOptions{})
	if err != nil {
		panic(err)
	}
	return h, &Index{Index: index}
}

// MustOpenIndex returns a new, opened index at a temporary path, or
// fails the test. It also returns the holder containing the index.
func MustOpenIndex(tb testing.TB) (*Holder, *Index) {
	return newIndex(tb)
}

// Close closes the index and removes the underlying data.
func (i *Index) Close() error {
	return i.Index.Close()
}

// Reopen closes the index and reopens it.
func (i *Index) Reopen() error {
	if err := i.Index.Close(); err != nil {
		return err
	}
	schema, err := i.Holder().Schemator.Schema(context.Background())
	if err != nil {
		return err
	}
	return i.OpenWithSchema(schema[i.Name()])
}

// CreateField creates a field with the given options.
func (i *Index) CreateField(name string, opts ...pilosa.FieldOption) (*Field, error) {
	f, err := i.Index.CreateField(name, opts...)
	if err != nil {
		return nil, err
	}
	return &Field{Field: f}, nil
}

// CreateFieldIfNotExists creates a field with the given options if it doesn't exist.
func (i *Index) CreateFieldIfNotExists(name string, opts ...pilosa.FieldOption) (*Field, error) {
	f, err := i.Index.CreateFieldIfNotExists(name, opts...)
	if err != nil {
		return nil, err
	}
	return &Field{Field: f}, nil
}
