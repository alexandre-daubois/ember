package ui

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestSortedStatusCodes_Ordered(t *testing.T) {
	codes := map[int]float64{500: 1.0, 200: 10.0, 404: 3.0, 301: 2.0}
	result := sortedStatusCodes(codes)

	require.Len(t, result, 4)
	assert.Equal(t, 200, result[0].code)
	assert.Equal(t, 301, result[1].code)
	assert.Equal(t, 404, result[2].code)
	assert.Equal(t, 500, result[3].code)

	assert.Equal(t, 10.0, result[0].rate)
	assert.Equal(t, 1.0, result[3].rate)
}

func TestSortedStatusCodes_Empty(t *testing.T) {
	assert.Empty(t, sortedStatusCodes(nil))
	assert.Empty(t, sortedStatusCodes(map[int]float64{}))
}

func TestSortedStatusCodes_SingleCode(t *testing.T) {
	result := sortedStatusCodes(map[int]float64{200: 5.0})

	require.Len(t, result, 1)
	assert.Equal(t, 200, result[0].code)
	assert.Equal(t, 5.0, result[0].rate)
}

func TestSortedMethods_OrderedByRateDesc(t *testing.T) {
	methods := map[string]float64{"GET": 10.0, "POST": 50.0, "DELETE": 1.0}
	result := sortedMethods(methods)

	require.Len(t, result, 3)
	assert.Equal(t, "POST", result[0].method)
	assert.Equal(t, 50.0, result[0].rate)
	assert.Equal(t, "GET", result[1].method)
	assert.Equal(t, "DELETE", result[2].method)
}

func TestSortedMethods_Empty(t *testing.T) {
	assert.Empty(t, sortedMethods(nil))
	assert.Empty(t, sortedMethods(map[string]float64{}))
}

func TestSortedMethods_SingleMethod(t *testing.T) {
	result := sortedMethods(map[string]float64{"PUT": 3.5})

	require.Len(t, result, 1)
	assert.Equal(t, "PUT", result[0].method)
	assert.Equal(t, 3.5, result[0].rate)
}

func TestFormatDetailRate_Zero(t *testing.T) {
	assert.Equal(t, "—", formatDetailRate(0))
}

func TestFormatDetailRate_Positive(t *testing.T) {
	result := formatDetailRate(42.5)
	assert.Contains(t, result, "/s")
}

func TestFormatDetailRate_SmallValue(t *testing.T) {
	result := formatDetailRate(0.5)
	assert.Contains(t, result, "/s")
}
