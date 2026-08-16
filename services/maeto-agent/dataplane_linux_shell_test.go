package maetoagent

import "testing"

func Test_GetDefaultRouteAndDev(t *testing.T) {
	dp := NewLinuxShellDataplane()

	route, dev, err := dp.GetDefaultRouteAndDev(t.Context())
	if err != nil {
		t.Fatalf("failed to get route and/or dev: %v", err)
	}

	t.Logf("Default Route: %s | Default Dev: %s", route, dev)
}
