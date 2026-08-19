package maetoagent

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"os/exec"
	"strings"
)

const vrfUnreachableMetric = "4278198272"

var _ Dataplane = (*LinuxShellDataplane)(nil)

type DefaultRouteOutput struct {
	Gateway string `json:"gateway"`
	Dev     string `json:"dev"`
}

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

func runWithOutput(ctx context.Context, name string, args ...string) ([]byte, error) {
	//nolint:gosec // G204: Shell wrapper requires variable execution path
	cmd := exec.CommandContext(ctx, name, args...)
	var stderr bytes.Buffer
	cmd.Stderr = &stderr

	out, err := cmd.Output()
	if err != nil {
		return nil, fmt.Errorf("%s %s: %w: %s", name, strings.Join(args, " "), err, strings.TrimSpace(stderr.String()))
	}

	return out, nil
}

func (ls *LinuxShellDataplane) AddVRF(ctx context.Context, tableName string, tableId int) (err error) {
	if err := run(ctx, "ip", "link", "add", tableName, "type", "vrf", "table", fmt.Sprintf("%d", tableId)); err != nil {
		return fmt.Errorf("create vrf device: %w", err)
	}

	if err := run(ctx, "ip", "link", "set", "dev", tableName, "up"); err != nil {
		return fmt.Errorf("bring up vrf device: %w", err)
	}

	// This prevents route lookup from falling through when
	// it doesn't match a route. Weird Linux VRF implementation thing
	if err := run(ctx, "ip", "-6", "route", "replace", "unreachable", "default",
		"vrf", tableName, "metric", vrfUnreachableMetric); err != nil {
		return fmt.Errorf("install vrf catch-all for %s: %w", tableName, err)
	}

	// https://onvox.net/2024/12/16/srv6-frr/#usid-caveat
	// It is important to note that [net.vrf.strict_mode=1] setting gets reset any
	// 	time a new VRF is added to the kernel and must be reset again
	if err := run(ctx, "sysctl", "-w", "net.vrf.strict_mode=1"); err != nil {
		return fmt.Errorf("enable vrf strict mode: %w", err)
	}

	return nil
}

func (ls *LinuxShellDataplane) InsertXFRMInterface(ctx context.Context, interfaceName string, underLayIface string, ifID uint32, vrfTableName string) error {
	// ip link add ipsec1 type xfrm dev eth1 if_id 1
	// ip link set ipsec1 master vrf-tenant-1
	// ip link set ipsec1 up

	if err := run(ctx, "ip", "link", "add", interfaceName, "type", "xfrm", "dev", underLayIface, "if_id", fmt.Sprint(ifID)); err != nil {
		return fmt.Errorf("create xfrm device: %w", err)
	}

	if err := run(ctx, "ip", "link", "set", interfaceName, "master", vrfTableName); err != nil {
		return fmt.Errorf("set xfrm device master to vrf: %w", err)
	}

	if err := run(ctx, "ip", "link", "set", interfaceName, "up"); err != nil {
		return fmt.Errorf("bring up xfrm device: %w", err)
	}

	return nil
}

func (ls *LinuxShellDataplane) InsertReturnPrefix(ctx context.Context, tunnelIface string, vrfTableName string, prefix string) error {
	// ip -6 route replace fd7a:3921:e7:1::/64 dev xfrm-231-1 vrf vrf-tenant-231

	if err := run(ctx, "ip", "-6", "route", "replace", prefix, "dev", tunnelIface, "vrf", vrfTableName); err != nil {
		return fmt.Errorf("insert return prefix %s into vrf %s @ %s: %w", prefix, vrfTableName, tunnelIface, err)
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

func (ls *LinuxShellDataplane) GetDefaultRouteAndDev(ctx context.Context) (string, string, error) {
	rawOutput, err := runWithOutput(ctx, "ip", "-6", "-j", "route", "show", "default")
	if err != nil {
		return "", "", fmt.Errorf("failed to get ip route default: %w", err)
	}

	parsedOutput := make([]DefaultRouteOutput, 1)
	err = json.Unmarshal(rawOutput, &parsedOutput)

	if err != nil {
		return "", "", fmt.Errorf("failed to parse JSON output: %w", err)
	}

	return parsedOutput[0].Gateway, parsedOutput[0].Dev, nil
}
