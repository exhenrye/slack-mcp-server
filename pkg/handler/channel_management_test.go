package handler

import (
	"testing"

	"github.com/slack-go/slack"
)

func TestResolveUserID(t *testing.T) {
	users := map[string]slack.User{
		"U1": {ID: "U1", Name: "evandro", RealName: "Evandro Mendonça"},
		"U2": {ID: "U2", Name: "luca", RealName: "Luca Luzzatti", Profile: slack.UserProfile{DisplayName: "Luca"}},
		"U3": {ID: "U3", Name: "luca.b", RealName: "Luca Bianchi", Profile: slack.UserProfile{DisplayName: "Luca"}},
		"U4": {ID: "U4", Name: "gone", RealName: "Former Person", Deleted: true},
	}
	usersInv := map[string]string{"evandro": "U1", "luca": "U2", "luca.b": "U3", "gone": "U4"}

	cases := []struct {
		in      string
		want    string
		wantErr bool
	}{
		{"U1", "U1", false},
		{"U0BGXPH12MQ", "U0BGXPH12MQ", false}, // real ID missing from the user cache
		{"UNKNOWN", "", true},                 // too short to be an ID
		{"@evandro", "U1", false},
		{"evandro", "U1", false},
		{"evandro mendonça", "U1", false},
		{"Luca Luzzatti", "U2", false},
		{"Luca", "U2", false}, // handle "luca" wins before display-name matching
		{"Former Person", "", true},
		{"Nobody", "", true},
	}
	for _, c := range cases {
		got, err := resolveUserID(c.in, users, usersInv)
		if (err != nil) != c.wantErr || got != c.want {
			t.Errorf("resolveUserID(%q) = %q, %v; want %q, err=%v", c.in, got, err, c.want, c.wantErr)
		}
	}

	delete(usersInv, "luca")
	if _, err := resolveUserID("Luca", users, usersInv); err == nil {
		t.Error("expected ambiguity error for display name shared by two users")
	}
}

func TestChannelNamePattern(t *testing.T) {
	for _, ok := range []string{"prd-001-bulk-recipients", "a", "x_y"} {
		if !channelNamePattern.MatchString(ok) {
			t.Errorf("%q should be valid", ok)
		}
	}
	for _, bad := range []string{"", "Upper", "has space", "#hash"} {
		if channelNamePattern.MatchString(bad) {
			t.Errorf("%q should be invalid", bad)
		}
	}
}
