// Copyright 2019 the u-root Authors. All rights reserved
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package main

import (
	"flag"
	"fmt"
	"log"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/u-root/iscsinl"
	"github.com/u-root/u-root/pkg/cmdline"
	"github.com/u-root/u-root/pkg/dhclient"
	slaunch "github.com/u-root/u-root/pkg/securelaunch"
	"github.com/u-root/u-root/pkg/securelaunch/policy"
	"github.com/u-root/u-root/pkg/securelaunch/tpm"
)

var slDebug = flag.Bool("d", false, "enable debug logs")

// checkDebugFlag checks if `uroot.uinitargs=-d` is set on the kernel cmdline.
// If it is set, slaunch.Debug is set to log.Printf.
func checkDebugFlag() {
	// By default, CommandLine exits on error, but this makes it trivial to get
	// a shell in u-root. Instead, continue on error and let the error handling
	// code here handle it.
	flag.CommandLine.Init(flag.CommandLine.Name(), flag.ContinueOnError)

	flag.Parse()

	if flag.NArg() > 1 {
		log.Fatal("Incorrect number of arguments")
	}

	if *slDebug {
		slaunch.Debug = log.Printf
		slaunch.Debug("debug flag is set. Logging Enabled.")
	}
}

// iscsiSpecified checks if iscsi has been set on the kernel command line.
func iscsiSpecified() bool {
	return cmdline.ContainsFlag("netroot") && cmdline.ContainsFlag("rd.iscsi.initator")
}

// scanIscsiDrives calls dhcleint to parse cmdline and iscsinl to mount iscsi
// drives.
func scanIscsiDrives() error {
	uri, ok := cmdline.Flag("netroot")
	if !ok {
		return fmt.Errorf("could not get `netroot` argument")
	}
	slaunch.Debug("scanIscsiDrives: netroot flag is set: '%s'", uri)

	initiator, ok := cmdline.Flag("rd.iscsi.initiator")
	if !ok {
		return fmt.Errorf("could not get `rd.iscsi.initiator` argument")
	}
	slaunch.Debug("scanIscsiDrives: rd.iscsi.initiator flag is set: '%s'", initiator)

	target, volume, err := dhclient.ParseISCSIURI(uri)
	if err != nil {
		return fmt.Errorf("dhclient iSCSI parser failed: %w", err)
	}

	slaunch.Debug("scanIscsiDrives: resolved target: '%s'", target)
	slaunch.Debug("scanIscsiDrives: resolved volume: '%s'", volume)

	devices, err := iscsinl.MountIscsi(
		iscsinl.WithInitiator(initiator),
		iscsinl.WithTarget(target.String(), volume),
		iscsinl.WithCmdsMax(128),
		iscsinl.WithQueueDepth(16),
		iscsinl.WithScheduler("noop"),
	)
	if err != nil {
		return fmt.Errorf("could not mount iSCSI drive: %w", err)
	}

	for i := range devices {
		slaunch.Debug("scanIscsiDrives: iSCSI drive mounted at '%s'", devices[i])
	}

	return nil
}

// main parses platform policy file, and based on the inputs performs
// measurements and then launches a target kernel.
//
// Steps followed by uinit:
// 1. if debug flag is set, enable logging.
// 2. gets the TPM handle
// 3. Gets secure launch policy file entered by user.
// 4. calls collectors to collect measurements(hashes) a.k.a evidence.
func main() {
	// Ignore ctrl+c
	signal.Ignore(syscall.SIGINT)

	checkDebugFlag()

	// Check if an iSCSI drive was specified and if so, mount it.
	if iscsiSpecified() {
		if err := scanIscsiDrives(); err != nil {
			log.Printf("failed to mount iSCSI drive, err=%v", err)
			return
		}
	}

	defer unmountAndExit() // called only on error, on success we kexec
	slaunch.Debug("********Step 1: init completed. starting main ********")
	if err := tpm.New(); err != nil {
		log.Printf("tpm.New() failed. err=%v", err)
		return
	}
	defer tpm.Close()

	slaunch.Debug("********Step 2: locate and parse SL Policy ********")
	p, err := policy.Get()
	if err != nil {
		log.Printf("failed to get policy err=%v", err)
		return
	}
	slaunch.Debug("policy file successfully parsed")

	slaunch.Debug("********Step 3: Collecting Evidence ********")
	for _, c := range p.Collectors {
		slaunch.Debug("Input Collector: %v", c)
		if e := c.Collect(); e != nil {
			log.Printf("Collector %v failed, err = %v", c, e)
		}
	}
	slaunch.Debug("Collectors completed")

	slaunch.Debug("********Step 4: Measuring target kernel, initrd ********")
	if err := p.Launcher.MeasureKernel(); err != nil {
		log.Printf("Launcher.MeasureKernel failed err=%v", err)
		return
	}

	slaunch.Debug("********Step 5: Parse eventlogs *********")
	if err := p.EventLog.Parse(); err != nil {
		log.Printf("EventLog.Parse() failed err=%v", err)
		return
	}

	slaunch.Debug("*****Step 6: Dump logs to disk *******")
	if err := slaunch.ClearPersistQueue(); err != nil {
		log.Printf("ClearPersistQueue failed err=%v", err)
		return
	}

	slaunch.Debug("********Step *: Unmount all ********")
	slaunch.UnmountAll()

	slaunch.Debug("********Step 7: Launcher called to Boot ********")
	if err := p.Launcher.Boot(); err != nil {
		log.Printf("Boot failed. err=%s", err)
		return
	}
}

// unmountAndExit is called on error and unmounts all devices.
func unmountAndExit() {
	slaunch.UnmountAll()

	// Let queued up debug statements get printed.
	time.Sleep(5 * time.Second)

	os.Exit(1)
}
