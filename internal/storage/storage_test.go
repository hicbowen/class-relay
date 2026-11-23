package storage

import (
	"strings"
	"testing"
)

func TestWindowsSQLiteURI(t *testing.T) {
	dsn := sqliteURI("C:/Users/教师/App Data/A&B?/classrelay.sqlite")
	if !strings.HasPrefix(dsn, "file:///C:/Users/%E6%95%99%E5%B8%88/App%20Data/A&B%3F/classrelay.sqlite?") {
		t.Errorf("unexpected SQLite URI: %s", dsn)
	}
}
