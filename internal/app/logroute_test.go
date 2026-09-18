package app

import (
	"testing"

	"github.com/alexandre-daubois/ember/internal/fetcher"
	"github.com/alexandre-daubois/ember/internal/model"
	"github.com/stretchr/testify/assert"
)

func TestRouteLogEntry(t *testing.T) {
	access := model.NewLogBuffer(10)
	runtime := model.NewLogBuffer(10)
	routes := model.NewRouteAggregator()
	feed := model.NewMercureFeed(10)

	routeLogEntry(fetcher.ParseLogLine(`{"level":"info","ts":1.0,"logger":"http.log.access.log0","msg":"handled request","request":{"method":"GET","host":"a","uri":"/x"},"status":200}`), access, runtime, routes, feed)
	routeLogEntry(fetcher.ParseLogLine(`{"level":"info","ts":1.0,"logger":"http.handlers.mercure","msg":"New subscriber"}`), access, runtime, routes, feed)
	routeLogEntry(fetcher.ParseLogLine(`{"level":"debug","ts":1.0,"logger":"http.handlers.mercure","msg":"Update published","update":{"id":"u1","topics":["demo"],"data":"x"}}`), access, runtime, routes, feed)
	routeLogEntry(fetcher.ParseLogLine(`{"level":"debug","ts":1.0,"logger":"http.handlers.mercure","msg":"Connection closed by the client"}`), access, runtime, routes, feed)
	routeLogEntry(fetcher.ParseLogLine(`{"level":"info","ts":1.0,"logger":"admin","msg":"x"}`), access, runtime, routes, feed)

	assert.Equal(t, 1, access.Len())
	assert.Equal(t, 1, routes.BucketCount())
	assert.Equal(t, 2, runtime.Len(), "the hub's INFO line and the admin line, never its DEBUG lines")
	assert.Equal(t, 1, feed.Len())
}
