// Copyright 2019 the u-root Authors. All rights reserved
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package integration

import (
	"bytes"
	"encoding/json"
	"reflect"
	"testing"

	"github.com/u-root/u-root/pkg/multiboot"
)

func TestMultiboot(t *testing.T) {
	var serial wc
	q, cleanup := QEMUTest(t, &Options{
		Files: []string{
			"/home/circleci/kernel:kernel",
		},
		Cmds: []string{
			"github.com/u-root/u-root/cmds/init",
			"github.com/u-root/u-root/cmds/kexec",
		},
		Uinit: []string{
			`/bbin/kexec -l /kernel -e -d --module="/kernel foo=bar" --module="/bbin/bb"`,
		},
		SerialOutput: &serial,
	})
	defer cleanup()

	if err := q.Expect(`"status": "ok"`); err != nil {
		t.Fatal(`expected "HELLO WORLD", got error: `, err)
	}

	output := serial.Bytes()

	i := bytes.Index(output, []byte(multiboot.DebugPrefix))
	if i == -1 {
		t.Fatalf("%q prefix not found in output", multiboot.DebugPrefix)
	}
	output = output[i+len(multiboot.DebugPrefix):]
	if i = bytes.Index(output, []byte{'\n'}); i == -1 {
		t.Fatalf("Cannot find newline character")
	}
	var want multiboot.Description
	if err := json.Unmarshal(output[:i], &want); err != nil {
		t.Fatalf("Cannot unmarshal multiboot debug information: %v", err)
	}

	const starting = "Starting multiboot kernel"
	if i = bytes.Index(output, []byte(starting)); i == -1 {
		t.Fatalf("Multiboot kernel was not executed")
	}
	output = output[i+len(starting):]

	var got multiboot.Description
	if err := json.Unmarshal(output, &got); err != nil {
		t.Fatalf("Cannot unmarshal multiboot information from executed kernel: %v", err)
	}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("kexec failed: got %v, want %v", got, want)
	}
}
