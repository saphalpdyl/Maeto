package maetoagent

import (
	"bytes"
	"context"
	"fmt"
	"os/exec"
	"strings"
)

type LinuxShellDataplane struct{}

func NewLinuxShellDataplane() *LinuxShellDataplane {
	return &LinuxShellDataplane{}
}

func run(ctx context.Context, name string, args ...string) error {
	//nolint:gosec // G204: Shell wrapper requires variable execution path
	cmd := exec.CommandContext(ctx, name, args...)
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	if err := cmd.Run(); err != nil {
		return fmt.Errorf("%s %s: %w: %s", name, strings.Join(args, " "), err, strings.TrimSpace(stderr.String()))
	}
	return nil
}

func (ls *LinuxShellDataplane) AddVRF(ctx context.Context, tableName string, tableId string, iface string, dtsid string) (err error) {
	if err := run(ctx, "ip", "link", "add", tableName, "type", "vrf", "table", tableId); err != nil {
		return fmt.Errorf("create vrf device: %w", err)
	}

	if err := run(ctx, "ip", "link", "set", "dev", tableName, "up"); err != nil {
		return fmt.Errorf("bring up vrf device: %w", err)
	}

	if err := run(ctx, "ip", "link", "set", iface, "master", tableName); err != nil {
		return fmt.Errorf("enslave interface %s to %s: %w", iface, tableName, err)
	}

	// https://onvox.net/2024/12/16/srv6-frr/#usid-caveat
	// It is important to note that [net.vrf.strict_mode=1] setting gets reset any
	// 	time a new VRF is added to the kernel and must be reset again
	if err := run(ctx, "sysctl", "-w", "net.vrf.strict_mode=1"); err != nil {
		return fmt.Errorf("enable vrf strict mode: %w", err)
	}

	if err := run(ctx, "ip", "-6", "route", "replace", dtsid,
		"encap", "seg6local", "action", "End.DT6",
		"vrftable", tableId, "dev", tableName, "proto", "200"); err != nil {
		return fmt.Errorf("install End.DT6 decap route for %s: %w", dtsid, err)
	}

	return nil
}

func (ls *LinuxShellDataplane) UpsertPolicy(ctx context.Context, nhid string, dtsid string, vrfTableId string, vrfTableName string, sids []string) error {
	segs := append(append([]string{}, sids...), dtsid)

	if err := run(ctx, "ip", "-6", "nexthop", "replace", "id", nhid,
		"encap", "seg6", "mode", "encap",
		"segs", strings.Join(segs, ","),
		"dev", "sr0"); err != nil {
		return fmt.Errorf("upsert seg6 nexthop id %s: %w", nhid, err)
	}

	return nil
}

func (ls *LinuxShellDataplane) UpsertRouteToPolicy(ctx context.Context, dest string, vrfTableId string, nhid string) error {
	if err := run(ctx, "ip", "-6", "route", "replace", dest,
		"table", vrfTableId, "nhid", nhid); err != nil {
		return fmt.Errorf("upsert route %s -> nhid %s in table %s: %w", dest, nhid, vrfTableId, err)
	}

	return nil
}
