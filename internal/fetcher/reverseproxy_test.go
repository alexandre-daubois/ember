package fetcher

import (
	"encoding/json"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestParseReverseProxyConfigs_Full(t *testing.T) {
	raw := json.RawMessage(`{
		"apps": {
			"http": {
				"servers": {
					"srv0": {
						"routes": [{
							"handle": [{
								"handler": "reverse_proxy",
								"load_balancing": {
									"selection_policy": {"policy": "round_robin"}
								},
								"health_checks": {
									"active": {
										"uri": "/health",
										"interval": 5000000000
									}
								},
								"upstreams": [
									{"dial": "10.0.0.1:8080", "max_requests": 100},
									{"dial": "10.0.0.2:8080"}
								]
							}]
						}]
					}
				}
			}
		}
	}`)

	configs := ParseReverseProxyConfigs(raw)
	require.Len(t, configs, 1)

	rp := configs[0]
	assert.Equal(t, "round_robin", rp.LBPolicy)
	assert.Equal(t, "/health", rp.HealthURI)
	assert.Equal(t, "5s", rp.HealthInterval, "5 billion nanoseconds should format as 5s")
	require.Len(t, rp.Upstreams, 2)
	assert.Equal(t, "10.0.0.1:8080", rp.Upstreams[0].Address)
	assert.Equal(t, 100, rp.Upstreams[0].MaxRequests)
	assert.Equal(t, "10.0.0.2:8080", rp.Upstreams[1].Address)
	assert.Equal(t, 0, rp.Upstreams[1].MaxRequests)
}

func TestParseReverseProxyConfigs_MultipleHandlers(t *testing.T) {
	raw := json.RawMessage(`{
		"apps": {
			"http": {
				"servers": {
					"srv0": {
						"routes": [
							{
								"handle": [
									{"handler": "reverse_proxy", "upstreams": [{"dial": "a:80"}]},
									{"handler": "subroute"}
								]
							},
							{
								"handle": [
									{"handler": "reverse_proxy", "upstreams": [{"dial": "b:80"}]}
								]
							}
						]
					}
				}
			}
		}
	}`)

	configs := ParseReverseProxyConfigs(raw)
	require.Len(t, configs, 2)
	assert.Equal(t, "a:80", configs[0].Upstreams[0].Address)
	assert.Equal(t, "b:80", configs[1].Upstreams[0].Address)
}

func TestParseReverseProxyConfigs_NoReverseProxy(t *testing.T) {
	raw := json.RawMessage(`{
		"apps": {
			"http": {
				"servers": {
					"srv0": {
						"routes": [{
							"handle": [{"handler": "subroute"}]
						}]
					}
				}
			}
		}
	}`)

	configs := ParseReverseProxyConfigs(raw)
	assert.Empty(t, configs)
}

func TestParseReverseProxyConfigs_InvalidJSON(t *testing.T) {
	configs := ParseReverseProxyConfigs(json.RawMessage(`{invalid}`))
	assert.Nil(t, configs)
}

func TestParseReverseProxyConfigs_EmptyConfig(t *testing.T) {
	configs := ParseReverseProxyConfigs(json.RawMessage(`{}`))
	assert.Empty(t, configs)
}

func TestFormatNanoDuration(t *testing.T) {
	assert.Equal(t, "0ms", formatNanoDuration(0))
	assert.Equal(t, "500ms", formatNanoDuration(500_000_000))
	assert.Equal(t, "1s", formatNanoDuration(1_000_000_000))
	assert.Equal(t, "2s", formatNanoDuration(2_000_000_000))
	assert.Equal(t, "60s", formatNanoDuration(60_000_000_000))
}

func TestFormatNanoDuration_FractionalSecondsKeptInMs(t *testing.T) {
	assert.Equal(t, "1500ms", formatNanoDuration(1_500_000_000),
		"sub-second remainders must not be silently truncated to whole seconds")
	assert.Equal(t, "2750ms", formatNanoDuration(2_750_000_000))
}

func TestFormatNanoDuration_LargeIntervalKeepsPrecision(t *testing.T) {
	// 7 days worth of nanoseconds: 604_800_000_000_000. Above 2^53 territory
	// where float64 would start losing integer precision.
	const week = int64(7*24*60*60) * int64(time.Second/time.Nanosecond)
	assert.Equal(t, "604800s", formatNanoDuration(week))
}

func TestParseReverseProxyConfigs_IntervalAsString(t *testing.T) {
	raw := json.RawMessage(`{
		"apps": {
			"http": {
				"servers": {
					"srv0": {
						"routes": [{
							"handle": [{
								"handler": "reverse_proxy",
								"health_checks": {
									"active": {
										"uri": "/health",
										"interval": "5s"
									}
								},
								"upstreams": [{"dial": "a:80"}]
							}]
						}]
					}
				}
			}
		}
	}`)

	configs := ParseReverseProxyConfigs(raw)
	require.Len(t, configs, 1, "string interval must be parsed, not cause the handler to be skipped")
	assert.Equal(t, "5s", configs[0].HealthInterval)
	assert.Equal(t, "/health", configs[0].HealthURI)
}

func TestParseReverseProxyConfigs_IntervalInvalidString(t *testing.T) {
	raw := json.RawMessage(`{
		"apps": {
			"http": {
				"servers": {
					"srv0": {
						"routes": [{
							"handle": [{
								"handler": "reverse_proxy",
								"health_checks": {
									"active": {
										"uri": "/health",
										"interval": "not a duration"
									}
								},
								"upstreams": [{"dial": "a:80"}]
							}]
						}]
					}
				}
			}
		}
	}`)

	configs := ParseReverseProxyConfigs(raw)
	assert.Empty(t, configs, "invalid duration string should fail the whole handler cleanly")
}

func TestParseReverseProxyConfigs_NestedInSubroute(t *testing.T) {
	raw := json.RawMessage(`{
		"apps": {
			"http": {
				"servers": {
					"srv0": {
						"routes": [{
							"handle": [{
								"handler": "subroute",
								"routes": [{
									"handle": [{
										"handler": "reverse_proxy",
										"upstreams": [{"dial": "deep:80"}]
									}]
								}]
							}]
						}]
					}
				}
			}
		}
	}`)

	configs := ParseReverseProxyConfigs(raw)
	require.Len(t, configs, 1, "reverse_proxy nested inside subroute must be discovered")
	assert.Equal(t, "deep:80", configs[0].Upstreams[0].Address)
}

func TestParseReverseProxyConfigs_DeeplyNested(t *testing.T) {
	raw := json.RawMessage(`{
		"apps": {
			"http": {
				"servers": {
					"srv0": {
						"routes": [{
							"handle": [{
								"handler": "subroute",
								"routes": [{
									"handle": [{
										"handler": "subroute",
										"routes": [{
											"handle": [{
												"handler": "reverse_proxy",
												"upstreams": [{"dial": "deepest:80"}]
											}]
										}]
									}]
								}]
							}]
						}]
					}
				}
			}
		}
	}`)

	configs := ParseReverseProxyConfigs(raw)
	require.Len(t, configs, 1, "recursion must handle arbitrary subroute depth")
	assert.Equal(t, "deepest:80", configs[0].Upstreams[0].Address)
}

func TestParseReverseProxyConfigs_NoLBOrHealth(t *testing.T) {
	raw := json.RawMessage(`{
		"apps": {
			"http": {
				"servers": {
					"srv0": {
						"routes": [{
							"handle": [{
								"handler": "reverse_proxy",
								"upstreams": [{"dial": "backend:8080"}]
							}]
						}]
					}
				}
			}
		}
	}`)

	configs := ParseReverseProxyConfigs(raw)
	require.Len(t, configs, 1)
	assert.Empty(t, configs[0].LBPolicy)
	assert.Empty(t, configs[0].HealthURI)
	assert.Equal(t, "backend:8080", configs[0].Upstreams[0].Address)
}
