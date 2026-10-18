package controlplane

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

const (
	demandLabelVoice     uint64 = 2
	demandLabelStreaming uint64 = 3
)

type demandSite struct {
	cpe         string
	attach      string
	reservation float64
	classes     string
}

type demandTenant struct {
	id    TenantID
	tier  string
	sites []demandSite
}

func demandRepo(t *testing.T, tenants ...demandTenant) TenantRepository {
	t.Helper()

	ifID := 1
	tenantJSON := make([]string, 0, len(tenants))
	for _, tenant := range tenants {
		siteJSON := make([]string, 0, len(tenant.sites))
		for _, site := range tenant.sites {
			siteJSON = append(siteJSON, fmt.Sprintf(`{
				"cpe": %q, "portal_id": "portal%06d", "node": "Cpe%s",
				"prefix": "fd00:%d:%x::/64", "attach": %q, "attach_node": "Pop%s",
				"if_id": %d, "identity": "id-%s", "reservation_mbps": %g, "classes": %s
			}`, site.cpe, ifID, site.cpe, tenant.id, ifID, site.attach, site.attach, ifID, site.cpe, site.reservation, site.classes))
			ifID++
		}

		tenantJSON = append(tenantJSON, fmt.Sprintf(`{
			"id": %d, "allocation": "fd00:%d::/32", "tier": %q, "vrf_table": %d,
			"sites": [%s]
		}`, tenant.id, tenant.id, tenant.tier, tenant.id, strings.Join(siteJSON, ",")))
	}

	db := `{
		"classes": [
			{"name": "voice", "label": 2, "index": 2, "match": {"dscp": [46]}, "objective": {"latency": 0.5, "jitter": 0.5}},
			{"name": "streaming", "label": 3, "index": 1, "match": {"dscp": [26, 28, 30]}, "objective": {"latency": 0.2, "jitter": 0.8}}
		],
		"tiers": [{"name": "gold", "index": 2}, {"name": "silver", "index": 1}],
		"tenants": [` + strings.Join(tenantJSON, ",") + `]
	}`

	dir := t.TempDir()
	build := filepath.Join(dir, "build")
	require.NoError(t, os.MkdirAll(build, 0o755)) // #nosec G301
	require.NoError(t, os.WriteFile(filepath.Join(build, TENANT_DB_FILENAME), []byte(db), 0o600))

	state := filepath.Join(dir, "latest.json")
	require.NoError(t, os.WriteFile(state, []byte(`{"output": "`+build+`"}`), 0o600))

	repo := NewJSONTenantRepository(TenantRepositoryConfig{StatePath: state, TopologyDirPath: dir})
	require.NoError(t, repo.Load(context.Background()))

	return repo
}

func eightPopDemandRepo(t *testing.T) TenantRepository {
	return demandRepo(t,
		demandTenant{id: 273, tier: "gold", sites: []demandSite{
			{cpe: "cA", attach: "A", reservation: 700, classes: `{"voice": 210, "streaming": 140}`},
			{cpe: "cC", attach: "C", reservation: 600, classes: `{"voice": 180, "streaming": 120}`},
		}},
		demandTenant{id: 231, tier: "silver", sites: []demandSite{
			{cpe: "cA2", attach: "A", reservation: 250, classes: `{"voice": 50}`},
			{cpe: "cA3", attach: "A", reservation: 400, classes: `{"voice": 80}`},
			{cpe: "cA4", attach: "G", reservation: 300, classes: `{"voice": 60}`},
		}},
	)
}

func demandName(d Demand) string {
	return fmt.Sprintf("%d/%s/%s>%s", d.Tenant.ID, d.Class.Name, d.From, d.To)
}

func demandNames(demands []Demand) []string {
	out := make([]string, len(demands))
	for i, d := range demands {
		out[i] = demandName(d)
	}
	return out
}

func findDemand(t *testing.T, demands []Demand, name string) Demand {
	t.Helper()

	for _, d := range demands {
		if demandName(d) == name {
			return d
		}
	}

	require.Failf(t, "demand not found", "%s not in %v", name, demandNames(demands))
	return Demand{}
}

func Test_BuildDemandsOrdersNamedClassesThenTierThenClassThenDefault(t *testing.T) {
	demands := BuildDemands(eightPopDemandRepo(t))

	assert.Equal(t, []string{
		"273/voice/A>C",
		"273/voice/C>A",
		"273/streaming/A>C",
		"273/streaming/C>A",
		"231/voice/A>G",
		"231/voice/G>A",
		"273/default/A>C",
		"273/default/C>A",
		"231/default/A>G",
		"231/default/G>A",
	}, demandNames(demands))
}

func Test_BuildDemandsSumsClassesOfSitesSharingAPop(t *testing.T) {
	demands := BuildDemands(eightPopDemandRepo(t))

	voice := findDemand(t, demands, "231/voice/A>G")
	assert.Equal(t, 130.0, voice.FromMbps)
	assert.Equal(t, 60.0, voice.ToMbps)

	bulk := findDemand(t, demands, "231/default/A>G")
	assert.Equal(t, 520.0, bulk.FromMbps)
	assert.Equal(t, 240.0, bulk.ToMbps)

	reverse := findDemand(t, demands, "231/voice/G>A")
	assert.Equal(t, 60.0, reverse.FromMbps)
	assert.Equal(t, 130.0, reverse.ToMbps)
}

func Test_BuildDemandsSkipsClassMissingAtOneEnd(t *testing.T) {
	repo := demandRepo(t,
		demandTenant{id: 273, tier: "gold", sites: []demandSite{
			{cpe: "cA", attach: "A", reservation: 700, classes: `{"voice": 210, "streaming": 140}`},
			{cpe: "cC", attach: "C", reservation: 600, classes: `{"voice": 180}`},
		}},
	)

	assert.Equal(t, []string{
		"273/voice/A>C",
		"273/voice/C>A",
		"273/default/A>C",
		"273/default/C>A",
	}, demandNames(BuildDemands(repo)))
}

func Test_BuildDemandsSkipsDefaultWhenFullyCarved(t *testing.T) {
	repo := demandRepo(t,
		demandTenant{id: 273, tier: "gold", sites: []demandSite{
			{cpe: "cA", attach: "A", reservation: 200, classes: `{"voice": 200}`},
			{cpe: "cC", attach: "C", reservation: 600, classes: `{"voice": 180}`},
		}},
	)

	assert.Equal(t, []string{
		"273/voice/A>C",
		"273/voice/C>A",
	}, demandNames(BuildDemands(repo)))
}

func Test_BuildDemandsIgnoresTenantOnASinglePop(t *testing.T) {
	repo := demandRepo(t,
		demandTenant{id: 273, tier: "gold", sites: []demandSite{
			{cpe: "cA", attach: "A", reservation: 700, classes: `{"voice": 210}`},
			{cpe: "cA2", attach: "A", reservation: 300, classes: `{}`},
		}},
	)

	assert.Empty(t, BuildDemands(repo))
}

func Test_BuildDemandsSilverVoiceBeatsGoldBulk(t *testing.T) {
	demands := BuildDemands(eightPopDemandRepo(t))
	names := demandNames(demands)

	assert.Less(t,
		indexOf(names, "231/voice/A>G"),
		indexOf(names, "273/default/A>C"),
	)
}

func Test_BuildDemandsUnrankedTierSortsAfterRankedTiers(t *testing.T) {
	repo := demandRepo(t,
		demandTenant{id: 100, tier: "standard", sites: []demandSite{
			{cpe: "cX", attach: "A", reservation: 100, classes: `{"voice": 10}`},
			{cpe: "cY", attach: "B", reservation: 100, classes: `{"voice": 10}`},
		}},
		demandTenant{id: 231, tier: "silver", sites: []demandSite{
			{cpe: "cA2", attach: "A", reservation: 100, classes: `{"voice": 10}`},
			{cpe: "cA4", attach: "G", reservation: 100, classes: `{"voice": 10}`},
		}},
	)

	assert.Equal(t, []string{
		"231/voice/A>G",
		"231/voice/G>A",
		"100/voice/A>B",
		"100/voice/B>A",
		"231/default/A>G",
		"231/default/G>A",
		"100/default/A>B",
		"100/default/B>A",
	}, demandNames(BuildDemands(repo)))
}

func Test_BuildDemandsSameTierOrdersByClassBeforeTenant(t *testing.T) {
	repo := demandRepo(t,
		demandTenant{id: 273, tier: "gold", sites: []demandSite{
			{cpe: "cA", attach: "A", reservation: 100, classes: `{"voice": 10, "streaming": 10}`},
			{cpe: "cC", attach: "C", reservation: 100, classes: `{"voice": 10, "streaming": 10}`},
		}},
		demandTenant{id: 300, tier: "gold", sites: []demandSite{
			{cpe: "cB", attach: "B", reservation: 100, classes: `{"voice": 10}`},
			{cpe: "cD", attach: "D", reservation: 100, classes: `{"voice": 10}`},
		}},
	)

	names := demandNames(BuildDemands(repo))
	assert.Less(t, indexOf(names, "300/voice/B>D"), indexOf(names, "273/streaming/A>C"))
}

func Test_BuildDemandsIsDeterministic(t *testing.T) {
	repo := eightPopDemandRepo(t)
	first := demandNames(BuildDemands(repo))

	for range 50 {
		assert.Equal(t, first, demandNames(BuildDemands(repo)))
	}
}

func Test_DemandKeysCarryTheClassLabel(t *testing.T) {
	demands := BuildDemands(eightPopDemandRepo(t))
	voice := findDemand(t, demands, "273/voice/A>C")

	assert.Equal(t, PathKey{Tenant: 273, From: "A", To: "C", Label: demandLabelVoice}, voice.PathKey())

	reservation := voice.BandwidthReservation()
	assert.Equal(t, BandwidthReservationKey{SourcePop: "A", SinkPop: "C", Label: demandLabelVoice}, reservation.Key)
	assert.Equal(t, 210.0, reservation.SourceBandwidth)
	assert.Equal(t, 180.0, reservation.SinkBandwidth)

	streaming := findDemand(t, demands, "273/streaming/C>A")
	assert.Equal(t, demandLabelStreaming, streaming.PathKey().Label)
	assert.Equal(t, 120.0, streaming.BandwidthReservation().SourceBandwidth)
	assert.Equal(t, 140.0, streaming.BandwidthReservation().SinkBandwidth)
}

func indexOf(names []string, name string) int {
	for i, n := range names {
		if n == name {
			return i
		}
	}
	return -1
}
