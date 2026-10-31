package main

import "testing"

func init() { defaultInviteMode = "rw" }

func TestInviteBornReadOnlyByDefault(t *testing.T) {
	defaultInviteMode = "ro"
	t.Cleanup(func() { defaultInviteMode = "rw" })
	dir := t.TempDir()
	shareStore, _ := openShares(dir)
	_, invitation := mkInv(t, shareStore)
	if invitation.Mode != "ro" {
		t.Errorf("mode %q", invitation.Mode)
	}
}
