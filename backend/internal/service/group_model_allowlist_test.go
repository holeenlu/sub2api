package service

import (
	"testing"

	"github.com/Wei-Shaw/sub2api/internal/catalog"
)

func TestGroupModelAllowlistEnabled(t *testing.T) {
	var nilGroup *Group
	if nilGroup.ModelAllowlistEnabled() {
		t.Fatal("nil group must not report enabled")
	}
	group := &Group{}
	if group.ModelAllowlistEnabled() {
		t.Fatal("default group must not report enabled")
	}
	group.ModelAllowlist = catalog.ModelAllowlist{Enabled: true, Models: []string{"m"}}
	if !group.ModelAllowlistEnabled() {
		t.Fatal("enabled group must report enabled")
	}
}
