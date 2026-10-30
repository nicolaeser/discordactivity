package voice

import (
	"fmt"
	"strings"
)

// Keepalive is how often the gateway re-affirms voice state while joined.
// Gateway-only joins (no voice UDP) stay connected with periodic op 4.
const Keepalive = "15s"

// Config describes one account's voice channel membership.
type Config struct {
	Enabled   bool
	GuildID   string
	ChannelID string
	SelfMute  bool
	SelfDeaf  bool
}

// WantJoin reports whether this config asks to be in a voice channel.
func (c Config) WantJoin() bool {
	return c.Enabled && strings.TrimSpace(c.ChannelID) != ""
}

// Validate checks the config. Guild may be empty (auto-resolved from the
// channel via the Discord API); when enabled the channel is required.
func (c Config) Validate() error {
	guild := strings.TrimSpace(c.GuildID)
	channel := strings.TrimSpace(c.ChannelID)
	if !c.Enabled {
		if guild != "" && !IsSnowflake(guild) {
			return fmt.Errorf("voice guild id: want numeric snowflake")
		}
		if channel != "" && !IsSnowflake(channel) {
			return fmt.Errorf("voice channel id: want numeric snowflake")
		}
		return nil
	}
	if channel == "" {
		return fmt.Errorf("voice channel id required when voice is enabled")
	}
	if !IsSnowflake(channel) {
		return fmt.Errorf("voice channel id: want numeric snowflake")
	}
	if guild != "" && !IsSnowflake(guild) {
		return fmt.Errorf("voice guild id: want numeric snowflake")
	}
	return nil
}

// IsSnowflake reports whether s looks like a Discord snowflake id.
func IsSnowflake(s string) bool {
	s = strings.TrimSpace(s)
	if len(s) < 15 || len(s) > 25 {
		return false
	}
	for i := 0; i < len(s); i++ {
		if s[i] < '0' || s[i] > '9' {
			return false
		}
	}
	return true
}

// JoinPayload builds the op 4 "d" object that joins (or moves to) a channel.
func JoinPayload(guild, channel string, mute, deaf bool) map[string]any {
	return map[string]any{
		"guild_id":   strings.TrimSpace(guild),
		"channel_id": strings.TrimSpace(channel),
		"self_mute":  mute,
		"self_deaf":  deaf,
	}
}

// LeavePayload builds the op 4 "d" object that leaves a guild's voice.
func LeavePayload(guild string) map[string]any {
	return map[string]any{
		"guild_id":   strings.TrimSpace(guild),
		"channel_id": nil,
		"self_mute":  false,
		"self_deaf":  false,
	}
}

// Normalize trims the id fields in place.
func (c *Config) Normalize() {
	c.GuildID = strings.TrimSpace(c.GuildID)
	c.ChannelID = strings.TrimSpace(c.ChannelID)
}
