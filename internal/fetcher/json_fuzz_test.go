package fetcher

import (
	"encoding/json"
	"testing"
)

func FuzzDecodeThreadsResponse(f *testing.F) {
	f.Add([]byte(`{"ThreadDebugStates":[{"Index":0,"Name":"worker","State":"ready","IsBusy":true}],"ReservedThreadCount":2}`))
	f.Add([]byte(`{"ThreadDebugStates":[],"ReservedThreadCount":0}`))
	f.Add([]byte(`{}`))
	f.Add([]byte(`{"ThreadDebugStates":null}`))
	f.Add([]byte(`{"ThreadDebugStates":[{"Index":-1,"MemoryUsage":-999}]}`))
	f.Add([]byte(`{"ThreadDebugStates":[{"CurrentURI":"` + string(make([]byte, 1000)) + `"}]}`))

	f.Fuzz(func(t *testing.T, data []byte) {
		var resp ThreadsResponse
		if json.Unmarshal(data, &resp) != nil {
			return
		}

		for i, thread := range resp.ThreadDebugStates {
			if thread.IsBusy && thread.IsWaiting {
				// both flags set simultaneously: unusual but should not panic
				_ = thread.State
			}
			_ = i
		}
	})
}

func FuzzExtractListenPorts(f *testing.F) {
	f.Add([]byte(`{"srv0":{"listen":[":443",":8080"]}}`))
	f.Add([]byte(`{"srv0":{"listen":[":80"]}}`))
	f.Add([]byte(`{"srv0":{}}`))
	f.Add([]byte(`{}`))
	f.Add([]byte(`{"srv0":{"listen":["localhost:2019","0.0.0.0:443"]}}`))
	f.Add([]byte(`{"srv0":{"listen":["not-a-port"]}}`))
	f.Add([]byte(`{"a":{"listen":[":1"]},"b":{"listen":[":1",":2"]}}`))

	f.Fuzz(func(t *testing.T, data []byte) {
		var servers map[string]json.RawMessage
		if json.Unmarshal(data, &servers) != nil {
			return
		}

		ports := extractListenPorts(servers)
		seen := make(map[string]struct{}, len(ports))
		for _, p := range ports {
			if p == "" {
				t.Fatal("extractListenPorts returned empty port")
			}
			if _, dup := seen[p]; dup {
				t.Fatalf("extractListenPorts returned duplicate port: %s", p)
			}
			seen[p] = struct{}{}
		}
	})
}
