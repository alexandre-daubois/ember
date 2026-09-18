package fetcher

import (
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func mercureRaw(handler string) json.RawMessage {
	return json.RawMessage(`{"apps":{"http":{"servers":{"srv0":{"routes":[{"handle":[{"handler":"subroute","routes":[{"handle":[` + handler + `]}]}]}]}}}}}`)
}

func TestParseMercureConfigs_Full(t *testing.T) {
	configs := ParseMercureConfigs(mercureRaw(`{
		"handler": "mercure",
		"name": "hub-1",
		"anonymous": true,
		"subscriptions": true,
		"debugger": true,
		"heartbeat": 40000000000,
		"write_timeout": "10m",
		"dispatch_timeout": 5000000000,
		"protocol_version_compatibility": 7,
		"issuers": [{"identifier": "https://issuer.example"}, {"identifier": "https://other.example"}],
		"cors_origins": ["https://app.example"],
		"publish_origins": ["*"],
		"public_urls": ["https://hub.example"],
		"transport": {"name": "local"}
	}`))
	require.Len(t, configs, 1)

	c := configs[0]
	assert.Equal(t, "hub-1", c.Name)
	assert.Equal(t, "local", c.Transport)
	assert.True(t, c.Anonymous)
	assert.True(t, c.Subscriptions)
	assert.True(t, c.Debugger)
	assert.False(t, c.Playground)
	assert.Equal(t, "40s", c.Heartbeat)
	assert.Equal(t, "600s", c.WriteTimeout)
	assert.Equal(t, "5s", c.DispatchTimeout)
	assert.Equal(t, 7, c.ProtocolCompat)
	assert.Equal(t, []string{"https://issuer.example", "https://other.example"}, c.Issuers)
	assert.Equal(t, []string{"https://app.example"}, c.CORSOrigins)
	assert.Equal(t, []string{"*"}, c.PublishOrigins)
	assert.Equal(t, []string{"https://hub.example"}, c.PublicURLs)
}

func TestParseMercureConfigs_DefaultsToBolt(t *testing.T) {
	configs := ParseMercureConfigs(mercureRaw(`{"handler": "mercure"}`))
	require.Len(t, configs, 1)

	c := configs[0]
	assert.Equal(t, "bolt", c.Transport)
	assert.Empty(t, c.Heartbeat)
	assert.Empty(t, c.Issuers)
	assert.False(t, c.Debugger)
}

func TestParseMercureConfigs_PlaygroundImpliesDebugger(t *testing.T) {
	configs := ParseMercureConfigs(mercureRaw(`{"handler": "mercure", "playground": true}`))
	require.Len(t, configs, 1)
	assert.True(t, configs[0].Playground)
	assert.True(t, configs[0].Debugger)
}

func TestParseMercureConfigs_DeprecatedTransportURL(t *testing.T) {
	configs := ParseMercureConfigs(mercureRaw(`{"handler": "mercure", "transport_url": "redis://localhost:6379"}`))
	require.Len(t, configs, 1)
	assert.Equal(t, "redis", configs[0].Transport)
}

func TestParseMercureConfigs_LegacyJWTKeysAreNotExposed(t *testing.T) {
	configs := ParseMercureConfigs(mercureRaw(`{
		"handler": "mercure",
		"publisher_jwt": {"key": "!ChangeThisMercureHubJWTSecretKey!", "alg": "HS256"},
		"subscriber_jwks_url": "https://issuer.example/jwks"
	}`))
	require.Len(t, configs, 1)
	assert.Equal(t, []string{"legacy publisher HS256 subscriber JWKS"}, configs[0].Issuers)
	assert.NotContains(t, configs[0].Issuers[0], "ChangeThis")
}

func TestParseMercureConfigs_MultipleHubs(t *testing.T) {
	raw := json.RawMessage(`{"apps":{"http":{"servers":{
		"srv0":{"routes":[{"handle":[{"handler":"mercure","name":"a"}]}]},
		"srv1":{"routes":[{"handle":[{"handler":"mercure","name":"b"}]}]}
	}}}}`)
	configs := ParseMercureConfigs(raw)
	require.Len(t, configs, 2)
	names := []string{configs[0].Name, configs[1].Name}
	assert.ElementsMatch(t, []string{"a", "b"}, names)
}

func TestParseMercureConfigs_NoMercure(t *testing.T) {
	assert.Empty(t, ParseMercureConfigs(mercureRaw(`{"handler": "reverse_proxy", "upstreams": [{"dial": "a:80"}]}`)))
	assert.Empty(t, ParseMercureConfigs(json.RawMessage(`{}`)))
	assert.Nil(t, ParseMercureConfigs(json.RawMessage(`{invalid}`)))
}

func TestMercureTransportName(t *testing.T) {
	assert.Equal(t, "redis", mercureTransportName("redis", "bolt://x"))
	assert.Equal(t, "bolt", mercureTransportName("", "bolt:///data/mercure.db"))
	assert.Equal(t, "not-a-url", mercureTransportName("", "not-a-url"))
	assert.Equal(t, "bolt", mercureTransportName("", ""))
}
