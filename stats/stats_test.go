// Copyright 2022 Molecula Corp. (DBA FeatureBase).
// SPDX-License-Identifier: Apache-2.0
package stats_test

import (
	"context"
	"fmt"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	pilosa "github.com/featurebasedb/featurebase/v3"
	"github.com/featurebasedb/featurebase/v3/logger"
	"github.com/featurebasedb/featurebase/v3/stats"
	"github.com/featurebasedb/featurebase/v3/test"
)

// TestMultiStatClient_Expvar run the multistat client with exp var
// since the EXPVAR data is stored in a global we should run these in one test function
func TestMultiStatClient_Expvar(t *testing.T) {
	hldr := test.MustOpenHolder(t)

	c := stats.NewExpvarStatsClient()
	ms := make(stats.MultiStatsClient, 1)
	ms[0] = c
	hldr.Stats = ms

	hldr.SetBit("d", "f", 0, 0)
	hldr.SetBit("d", "f", 0, 1)
	hldr.SetBit("d", "f", 0, pilosa.ShardWidth)
	hldr.SetBit("d", "f", 0, pilosa.ShardWidth+2)
	hldr.ClearBit("d", "f", 0, 1)

	indexStats := fmt.Sprintf(`{"%s": %d, "%s": %d}`, pilosa.MetricClearBit, 1, pilosa.MetricSetBit, 4)

	if stats.Expvar.String() != `{"index:d": `+indexStats+`}` {
		t.Fatalf("unexpected expvar : %s", stats.Expvar.String())
	}

	hldr.Stats.CountWithCustomTags("cc", 1, 1.0, []string{"foo:bar"})
	if stats.Expvar.String() != `{"cc": 1, "index:d": `+indexStats+`}` {
		t.Fatalf("unexpected expvar : %s", stats.Expvar.String())
	}

	// Gauge creates a unique key, subsequent Gauge calls will overwrite
	hldr.Stats.Gauge("g", 5, 1.0)
	hldr.Stats.Gauge("g", 8, 1.0)
	if stats.Expvar.String() != `{"cc": 1, "g": 8, "index:d": `+indexStats+`}` {
		t.Fatalf("unexpected expvar : %s", stats.Expvar.String())
	}

	// Set creates a unique key, subsequent sets will overwrite
	hldr.Stats.Set("s", "4", 1.0)
	hldr.Stats.Set("s", "7", 1.0)
	if stats.Expvar.String() != `{"cc": 1, "g": 8, "index:d": `+indexStats+`, "s": "7"}` {
		t.Fatalf("unexpected expvar : %s", stats.Expvar.String())
	}

	// Record timing duration and a uniquely Set key/value
	dur, _ := time.ParseDuration("123us")
	hldr.Stats.Timing("tt", dur, 1.0)
	if stats.Expvar.String() != `{"cc": 1, "g": 8, "index:d": `+indexStats+`, "s": "7", "tt": 123µs}` {
		t.Fatalf("unexpected expvar : %s", stats.Expvar.String())
	}

	// Expvar histogram is implemented as a gauge
	hldr.Stats.Histogram("hh", 3, 1.0)
	if stats.Expvar.String() != `{"cc": 1, "g": 8, "hh": 3, "index:d": `+indexStats+`, "s": "7", "tt": 123µs}` {
		t.Fatalf("unexpected expvar : %s", stats.Expvar.String())
	}

	// Expvar should ignore earlier set tags from setbit
	if hldr.Stats.Tags() != nil {
		t.Fatalf("unexpected tag")
	}
}

func TestStatsCount_TopN(t *testing.T) {
	c := test.MustRunCluster(t, 1)
	defer c.Close()
	hldr := test.Holder{Holder: c.GetNode(0).Server.Holder()}

	// Execute query.
	called := false
	hldr.Holder.Stats = &MockStats{
		mockCountWithTags: func(name string, value int64, rate float64, tags []string) {
			if name != "query_topn_total" {
				t.Errorf("Expected query_topn_total, Results %s", name)
			}

			if tags[0] != "index:d" {
				t.Errorf("Expected index, Results %s", tags[0])
			}

			called = true
		},
	}

	hldr.SetBit("d", "f", 0, 0)
	hldr.SetBit("d", "f", 0, 1)
	hldr.SetBit("d", "f", 0, pilosa.ShardWidth)
	hldr.SetBit("d", "f", 0, pilosa.ShardWidth+2)

	if _, err := c.GetNode(0).API.Query(context.Background(), &pilosa.QueryRequest{Index: "d", Query: `TopN(field=f, n=2)`}); err != nil {
		t.Fatal(err)
	}
	if !called {
		t.Error("CountWithCustomTags name isn't called")
	}
}

func TestStatsCount_Bitmap(t *testing.T) {
	// Cluster has to be unhsared because we're mocking the stats which writes
	// to a holder in use by other tests.
	c := test.MustRunUnsharedCluster(t, 1)
	defer c.Close()
	hldr := test.Holder{Holder: c.GetNode(0).Server.Holder()}
	called := false
	hldr.Holder.Stats = &MockStats{
		mockCountWithTags: func(name string, value int64, rate float64, tags []string) {
			if name != pilosa.MetricRow {
				t.Errorf("Expected %s, Results %s", pilosa.MetricRow, name)
			}

			if tags[0] != "index:d" {
				t.Errorf("Expected index, Results %s", tags[0])
			}

			called = true
		},
	}

	hldr.SetBit("d", "f", 0, 0)
	hldr.SetBit("d", "f", 0, 1)

	if _, err := c.GetNode(0).API.Query(context.Background(), &pilosa.QueryRequest{Index: "d", Query: `Row(f=0)`}); err != nil {
		t.Fatal(err)
	}
	if !called {
		t.Error("CountWithCustomTags name isn't called")
	}
}

func TestStatsCount_APICalls(t *testing.T) {
	// We can't share a cluster when we're modifying its stats counter.
	cluster := test.MustRunUnsharedCluster(t, 1)
	defer cluster.Close()
	cmd := cluster.GetNode(0)
	h := cmd.Handler.(*pilosa.Handler).Handler
	holder := cmd.Server.Holder()
	hldr := test.Holder{Holder: holder}

	t.Run("create index", func(t *testing.T) {
		called := false
		hldr.Stats = &MockStats{
			mockCount: func(name string, value int64, rate float64) {
				if name != pilosa.MetricCreateIndex {
					t.Errorf("Expected %v, Results %s", pilosa.MetricCreateIndex, name)
				}
				called = true
			},
		}
		w := httptest.NewRecorder()
		h.ServeHTTP(w, test.MustNewHTTPRequest("POST", "/index/i", strings.NewReader("")))
		if !called {
			t.Error("Count isn't called")
		}
	})

	t.Run("create field", func(t *testing.T) {
		called := false
		hldr.Stats = &MockStats{
			mockCountWithTags: func(name string, value int64, rate float64, index []string) {
				if name != pilosa.MetricCreateField {
					t.Errorf("Expected %v, Results %s", pilosa.MetricCreateField, name)
				}
				if index[0] != "index:i" {
					t.Errorf("Expected index:i, Results %s", index)
				}

				called = true
			},
		}
		w := httptest.NewRecorder()
		h.ServeHTTP(w, test.MustNewHTTPRequest("POST", "/index/i/field/f", strings.NewReader("")))
		if !called {
			t.Error("Count isn't called")
		}
	})

	t.Run("delete field", func(t *testing.T) {
		called := false
		hldr.Stats = &MockStats{
			mockCountWithTags: func(name string, value int64, rate float64, index []string) {
				if name != pilosa.MetricDeleteField {
					t.Errorf("Expected %v, Results %s", pilosa.MetricDeleteField, name)
				}
				if index[0] != "index:i" {
					t.Errorf("Expected index:i, Results %s", index)
				}

				called = true
			},
		}
		w := httptest.NewRecorder()
		h.ServeHTTP(w, test.MustNewHTTPRequest("DELETE", "/index/i/field/f", strings.NewReader("")))
		if !called {
			t.Error("Count isn't called")
		}
	})

	t.Run("delete index", func(t *testing.T) {
		called := false
		hldr.Stats = &MockStats{
			mockCount: func(name string, value int64, rate float64) {
				if name != pilosa.MetricDeleteIndex {
					t.Errorf("Expected %v, Results %s", pilosa.MetricDeleteIndex, name)
				}

				called = true
			},
		}
		w := httptest.NewRecorder()
		h.ServeHTTP(w, test.MustNewHTTPRequest("DELETE", "/index/i", strings.NewReader("")))
		if !called {
			t.Error("Count isn't called")
		}
	})

}

type MockStats struct {
	mockCount         func(name string, value int64, rate float64)
	mockCountWithTags func(name string, value int64, rate float64, tags []string)
}

func (s *MockStats) Count(name string, value int64, rate float64) {
	if s.mockCount != nil {
		s.mockCount(name, value, rate)
	}
}

func (s *MockStats) CountWithCustomTags(name string, value int64, rate float64, tags []string) {
	if s.mockCountWithTags != nil {
		s.mockCountWithTags(name, value, rate, tags)
	}
}

func (c *MockStats) Tags() []string                                        { return nil }
func (c *MockStats) WithTags(tags ...string) stats.StatsClient             { return c }
func (c *MockStats) Gauge(name string, value float64, rate float64)        {}
func (c *MockStats) Histogram(name string, value float64, rate float64)    {}
func (c *MockStats) Set(name string, value string, rate float64)           {}
func (c *MockStats) Timing(name string, value time.Duration, rate float64) {}
func (c *MockStats) SetLogger(logger logger.Logger)                        {}
func (c *MockStats) Open()                                                 {}
func (c *MockStats) Close() error                                          { return nil }
