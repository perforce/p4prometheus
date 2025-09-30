package main

import (
	"encoding/json"
	"reflect"
	"testing"
)

func TestFindLocks(t *testing.T) {
	lockdata := `{ "locks": [
		{"command": "lvmetad", "pid": "1458", "type": "POSIX", "size": "5B", "mode": "WRITE", "m": "0", "start": "0", "end": "0", "path": "/run/lvmetad.pid", "blocker": null},
		{"command": "p4d", "pid": "2502", "type": "FLOCK", "size": "17B", "mode": "READ", "m": "0", "start": "0", "end": "0", "path": "/p4/1/root/server.locks/clientEntity/10,d/robomerge-main-ts", "blocker": null},
		{"command": "p4d", "pid": "2502", "type": "FLOCK", "size": "17B", "mode": "READ", "m": "0", "start": "0", "end": "0", "path": "/p4/1/root/server.locks/meta/db", "blocker": null},
		{"command": "p4d", "pid": "2502", "type": "FLOCK", "size": "17B", "mode": "READ", "m": "0", "start": "0", "end": "0", "path": "/p4/1/root/db.have", "blocker": null}
	]}`
	mondata := `     562 I perforce 00:01:01 monitor
      2502 I fred 00:01:01 sync //...`
	metrics := findLocks(lockdata, mondata, nil)
	if metrics.DbReadLocks != 3 {
		t.Errorf("Expected 3 dbReadLocks, got %d", metrics.DbReadLocks)
	}
	if metrics.ClientEntityReadLocks != 1 {
		t.Errorf("Expected 1 clientEntityReadLocks, got %d", metrics.ClientEntityReadLocks)
	}
	if metrics.MetaReadLocks != 1 {
		t.Errorf("Expected 1 metaReadLocks, got %d", metrics.MetaReadLocks)
	}
	if metrics.BlockedCommands != 0 {
		t.Errorf("Expected 0 blockedCommands, got %d", metrics.BlockedCommands)
	}
	if len(metrics.Msgs) != 0 {
		t.Errorf("Expected 0 msgs, got %d", len(metrics.Msgs))
	}
}

func TestNoLocks(t *testing.T) {
	lockdata := `{}`
	mondata := `     562 I perforce 00:01:01 monitor
      2502 I fred 00:01:01 sync //...`
	metrics := findLocks(lockdata, mondata, nil)
	if metrics.DbReadLocks != 0 {
		t.Errorf("Expected 0 dbReadLocks, got %d", metrics.DbReadLocks)
	}
	if metrics.ClientEntityReadLocks != 0 {
		t.Errorf("Expected 0 clientEntityReadLocks, got %d", metrics.ClientEntityReadLocks)
	}
	if metrics.MetaReadLocks != 0 {
		t.Errorf("Expected 0 metaReadLocks, got %d", metrics.MetaReadLocks)
	}
	if metrics.BlockedCommands != 0 {
		t.Errorf("Expected 0 blockedCommands, got %d", metrics.BlockedCommands)
	}
	if len(metrics.Msgs) != 0 {
		t.Errorf("Expected 0 msgs, got %d", len(metrics.Msgs))
	}
}

func TestTextLslocksParse(t *testing.T) {
	lockdata := `COMMAND           PID   TYPE SIZE MODE  M START END PATH                       BLOCKER
(unknown)          -1 OFDLCK   0B WRITE 0     0   0 /etc/hosts
(unknown)          -1 OFDLCK   0B READ  0     0   0
p4d               107  FLOCK  16K READ* 0     0   0 /path/db.config            105
p4d               105  FLOCK  16K WRITE 0     0   0 /path/db.config
p4d               105  FLOCK  16K WRITE 0     0   0 /path/db.configh`
	jlock := parseTextLockInfo(lockdata)
	var got map[string]interface{}
	_ = json.Unmarshal([]byte(jlock), &got)
	expected := map[string]interface{}{
		"locks": []interface{}{
			map[string]interface{}{"command": "(unknown)", "pid": "-1", "type": "OFDLCK", "size": "0B", "mode": "WRITE", "m": "0", "start": "0", "end": "0", "path": "/etc/hosts", "blocker": nil},
			map[string]interface{}{"command": "p4d", "pid": "107", "type": "FLOCK", "size": "16K", "mode": "READ*", "m": "0", "start": "0", "end": "0", "path": "/path/db.config", "blocker": "105"},
			map[string]interface{}{"command": "p4d", "pid": "105", "type": "FLOCK", "size": "16K", "mode": "WRITE", "m": "0", "start": "0", "end": "0", "path": "/path/db.config", "blocker": nil},
			map[string]interface{}{"command": "p4d", "pid": "105", "type": "FLOCK", "size": "16K", "mode": "WRITE", "m": "0", "start": "0", "end": "0", "path": "/path/db.configh", "blocker": nil},
		},
	}
	if !reflect.DeepEqual(expected, got) {
		t.Errorf("Expected %v, got %v", expected, got)
	}
}

// Additional tests for blockers, metrics formatting, and edge cases can be added similarly.
