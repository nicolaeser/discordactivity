package voice

import "testing"

func TestValidate(t *testing.T) {
	t.Parallel()
	ok := Config{Enabled: true, GuildID: "123456789012345678", ChannelID: "123456789012345679", SelfMute: true}
	if err := ok.Validate(); err != nil {
		t.Fatal(err)
	}
	// Guild may be empty: resolved from the channel at runtime.
	guildless := Config{Enabled: true, ChannelID: "123456789012345679"}
	if err := guildless.Validate(); err != nil {
		t.Fatal(err)
	}
	if err := (Config{Enabled: true}).Validate(); err == nil {
		t.Fatal("expected channel required")
	}
	if err := (Config{Enabled: true, ChannelID: "abc"}).Validate(); err == nil {
		t.Fatal("expected snowflake error")
	}
	if err := (Config{Enabled: true, GuildID: "abc", ChannelID: "123456789012345679"}).Validate(); err == nil {
		t.Fatal("expected guild snowflake error")
	}
	if err := (Config{ChannelID: "not-an-id"}).Validate(); err == nil {
		t.Fatal("expected stored id validation")
	}
	if err := (Config{}).Validate(); err != nil {
		t.Fatal(err)
	}
}

func TestWantJoin(t *testing.T) {
	t.Parallel()
	if !(Config{Enabled: true, ChannelID: "123456789012345678"}).WantJoin() {
		t.Fatal("want join")
	}
	if (Config{Enabled: false, ChannelID: "123456789012345678"}).WantJoin() {
		t.Fatal("disabled must not join")
	}
	if (Config{Enabled: true}).WantJoin() {
		t.Fatal("no channel must not join")
	}
}

func TestPayloads(t *testing.T) {
	t.Parallel()
	j := JoinPayload("111", "222", true, false)
	if j["guild_id"] != "111" || j["channel_id"] != "222" || j["self_mute"] != true || j["self_deaf"] != false {
		t.Fatalf("%v", j)
	}
	l := LeavePayload("111")
	if l["guild_id"] != "111" || l["channel_id"] != nil {
		t.Fatalf("%v", l)
	}
}

func TestIsSnowflake(t *testing.T) {
	t.Parallel()
	for _, id := range []string{"123456789012345678", "1234567890123456789"} {
		if !IsSnowflake(id) {
			t.Fatalf("%s", id)
		}
	}
	for _, bad := range []string{"", "abc", "123", "12a456789012345678", "  "} {
		if IsSnowflake(bad) {
			t.Fatalf("accepts %q", bad)
		}
	}
}
