package swan

import (
	"testing"

	"github.com/strongswan/govici/vici"
)

func TestConnSectionUnwrapsListSA(t *testing.T) {
	child := vici.NewMessage()
	_ = child.Set("state", "INSTALLED")
	_ = child.Set("if-id-in", "00000002")
	_ = child.Set("if-id-out", "00000002")

	children := vici.NewMessage()
	_ = children.Set("cpe-2", child)

	ike := vici.NewMessage()
	_ = ike.Set("state", "ESTABLISHED")
	_ = ike.Set("remote-id", "5d9b7c30e1f8.cpe.maeto.net")
	_ = ike.Set("child-sas", children)

	m := vici.NewMessage()
	_ = m.Set("cpe", ike)

	conn, section, ok := connSection(m)
	if !ok || conn != "cpe" {
		t.Fatalf("connSection = %q, %v", conn, ok)
	}

	var sa IKESA
	if err := vici.UnmarshalMessage(section, &sa); err != nil {
		t.Fatal(err)
	}

	if sa.State != "ESTABLISHED" || sa.RemoteID != "5d9b7c30e1f8.cpe.maeto.net" {
		t.Fatalf("ike sa not filled: %+v", sa)
	}

	got := sa.ChildSAs["cpe-2"]
	id, err := got.IfID()
	if got.State != "INSTALLED" || err != nil || id != 2 {
		t.Fatalf("child sa = %+v, if_id %d, err %v", got, id, err)
	}
}
